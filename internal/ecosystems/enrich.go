package ecosystems

import (
	"context"
	"errors"
	"log"
)

// Store is the subset of the database the enrich loop needs. *db.DB satisfies it.
type Store interface {
	PendingEnrichment(limit int) ([]string, error)
	SetEnrichment(fullName, language, license string, topics []string) error
}

// Fetcher fetches a repository's metadata. *Client satisfies it.
type Fetcher interface {
	GetRepository(ctx context.Context, fullName string) (*Repository, error)
}

// Enrich processes pending repos through the fetcher, stamping each via the
// store. It stops when nothing is pending, the limit (>0) is reached, or ctx is
// cancelled. Returns the number stamped and the number skipped due to errors.
func Enrich(ctx context.Context, store Store, f Fetcher, limit int) (stamped, failed int, err error) {
	const batch = 500
	failedSet := map[string]bool{}
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

		progressed := false
		for _, name := range fresh {
			if ctx.Err() != nil {
				break
			}
			if limit > 0 && attempted >= limit {
				break
			}
			repo, gerr := f.GetRepository(ctx, name)
			attempted++
			switch {
			case gerr == nil:
				if e := store.SetEnrichment(name, repo.Language, repo.License, repo.Topics); e != nil {
					return stamped, len(failedSet), e
				}
				stamped++
				progressed = true
			case isNotFound(gerr):
				// Not on ecosyste.ms: stamp empty so we don't retry forever.
				if e := store.SetEnrichment(name, "", "", nil); e != nil {
					return stamped, len(failedSet), e
				}
				stamped++
				progressed = true
			case ctx.Err() != nil:
				// interrupted mid-request; leave pending
			default:
				// A single repo's error (e.g. a transient HTTP 500 from
				// ecosyste.ms) must not abort the whole run. Skip it — it stays
				// pending and is retried on the next run.
				log.Printf("skip %s after error: %v", name, gerr)
				failedSet[name] = true
			}
			if stamped%200 == 0 && progressed {
				log.Printf("enriched %d", stamped)
			}
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
