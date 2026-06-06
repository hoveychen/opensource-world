package ecosystems

import (
	"context"
	"errors"
	"log"
	"sync"

	"golang.org/x/sync/errgroup"
)

// enrichWorkers bounds how many repos are fetched concurrently. The client's
// rate limiter still caps the aggregate request rate, so this only governs how
// many requests may be in-flight at once — enough to keep the rate-limit budget
// saturated while some workers are parked in a 520 backoff or a slow response.
const enrichWorkers = 8

// Store is the subset of the database the enrich loop needs. *db.DB satisfies it.
// A nil repo means "no ecosyste.ms data" (e.g. a 404); the store still stamps the
// row as processed so it is not retried forever.
type Store interface {
	PendingEnrichment(limit int) ([]string, error)
	SetEnrichment(fullName string, repo *Repository) error
}

// Fetcher fetches a repository's metadata. *Client satisfies it.
type Fetcher interface {
	GetRepository(ctx context.Context, fullName string) (*Repository, error)
}

// Enrich processes pending repos through the fetcher, stamping each via the
// store. It stops when nothing is pending, the limit (>0) is reached, or ctx is
// cancelled. Returns the number stamped and the number skipped due to errors.
//
// Each batch is processed by a bounded worker pool, so a repo stuck retrying a
// transient 520 no longer blocks the repos behind it — other workers keep the
// rate-limited request pipeline full meanwhile.
func Enrich(ctx context.Context, store Store, f Fetcher, limit int) (stamped, failed int, err error) {
	const batch = 500
	failedSet := map[string]bool{}
	var mu sync.Mutex // guards failedSet, stamped, attempted
	attempted := 0

	for ctx.Err() == nil {
		if limit > 0 && attempted >= limit {
			break
		}
		names, perr := store.PendingEnrichment(batch + len(failedSet))
		if perr != nil {
			return stamped, len(failedSet), perr
		}
		fresh := names[:0:0]
		for _, n := range names {
			if !failedSet[n] {
				fresh = append(fresh, n)
			}
		}
		if len(fresh) == 0 {
			break
		}

		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(enrichWorkers)
		progressed := false
		for _, name := range fresh {
			if gctx.Err() != nil {
				break
			}
			name := name
			g.Go(func() error {
				// Reserve an attempt slot up-front so the limit is honored even
				// with workers racing; a goroutine past the limit is a no-op.
				mu.Lock()
				if (limit > 0 && attempted >= limit) || gctx.Err() != nil {
					mu.Unlock()
					return nil
				}
				attempted++
				mu.Unlock()

				repo, gerr := f.GetRepository(gctx, name)
				switch {
				case gerr == nil || isNotFound(gerr):
					// nil repo (404) is still stamped so it is not retried forever.
					var r *Repository
					if gerr == nil {
						r = repo
					}
					if e := store.SetEnrichment(name, r); e != nil {
						return e
					}
					mu.Lock()
					stamped++
					n := stamped
					progressed = true
					mu.Unlock()
					if n%200 == 0 {
						log.Printf("enriched %d", n)
					}
				case gctx.Err() != nil:
					// interrupted mid-request; leave pending for the next run
				default:
					// A single repo's transient error must not abort the run. Skip
					// it — it stays pending and is retried on the next run.
					log.Printf("skip %s after error: %v", name, gerr)
					mu.Lock()
					failedSet[name] = true
					mu.Unlock()
				}
				return nil
			})
		}
		if e := g.Wait(); e != nil {
			return stamped, len(failedSet), e
		}
		if !progressed {
			// Whole fresh batch failed without stamping anything: bail rather
			// than spin; the failures stay pending for the next run.
			break
		}
	}
	return stamped, len(failedSet), nil
}

func isNotFound(err error) bool {
	var nf *NotFound
	return errors.As(err, &nf)
}
