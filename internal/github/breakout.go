package github

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/hoveychen/opensource-world/internal/db"
)

// Defaults for the breakout sweep: repositories created within the last week
// that already cleared the watchlist floor.
//
// A repo created N days ago necessarily started at zero stars, so "went from 0
// to 1000+ stars in a week" needs no star history at all — created_at and the
// current star count fully define the set. That is why this sweep is a single
// cheap query rather than a snapshot-diffing pipeline.
const (
	// BreakoutWindow is how far back "new" reaches.
	BreakoutWindow = 7 * 24 * time.Hour
	// BreakoutFloor is the watchlist floor: repos below this never make either
	// board, so there is no reason to fetch them. Measured June-August 2026, a
	// week holds ~90-115 repos at >=300 stars — three pages, far under the
	// 1000-result cap.
	BreakoutFloor = 300
)

// BreakoutOptions configures the sweep for freshly created, fast-rising repos.
type BreakoutOptions struct {
	MinStars int           // watchlist floor (0 = BreakoutFloor)
	Window   time.Duration // how far back to look (0 = BreakoutWindow)
	Now      time.Time     // upper bound of the window (zero = time.Now)
}

func (o *BreakoutOptions) applyDefaults() {
	if o.MinStars <= 0 {
		o.MinStars = BreakoutFloor
	}
	if o.Window <= 0 {
		o.Window = BreakoutWindow
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	o.Now = o.Now.UTC()
}

// FetchBreakouts sweeps repositories created inside the trailing window that
// already hold at least MinStars, and upserts them into store. Unlike
// Enumerate, it keeps no window checkpoints: the sweep is a rolling snapshot
// meant to re-run on every crawl, refreshing star counts each time.
//
// It returns the number of repos stored.
func (c *Client) FetchBreakouts(ctx context.Context, store *db.DB, opts BreakoutOptions) (int, error) {
	return fetchBreakouts(ctx, c, store, opts)
}

// fetchBreakouts is the searcher-generic body, so tests can drive it with a fake
// instead of hitting GitHub.
func fetchBreakouts(ctx context.Context, s searcher, store *db.DB, opts BreakoutOptions) (int, error) {
	opts.applyDefaults()
	from := opts.Now.Add(-opts.Window)
	query := BuildBreakoutQuery(opts.MinStars, from, opts.Now)

	res, err := s.SearchRepositories(ctx, query, 1)
	if err != nil {
		return 0, err
	}
	total := res.TotalCount
	if total == 0 {
		return 0, nil
	}
	if total > resultCap {
		// Search hard-caps paging at 1000. Sorting is stars-DESC, so the repos
		// we lose are the lowest-star tail of the watchlist — the leaderboard
		// itself is never truncated. Raising MinStars restores full coverage.
		log.Printf("WARN: breakout window has %d repos > %d cap; keeping the top %d by stars (raise -min-stars)",
			total, resultCap, resultCap)
	}

	pages := int(math.Ceil(float64(min(total, resultCap)) / 100.0))
	stored := 0
	store1 := func(items []SearchItem) error {
		repos := make([]db.Repo, len(items))
		for i, it := range items {
			repos[i] = it.ToRepo()
		}
		stored += len(repos)
		return store.UpsertRepos(repos)
	}

	if err := store1(res.Items); err != nil {
		return stored, err
	}
	for p := 2; p <= pages; p++ {
		next, err := s.SearchRepositories(ctx, query, p)
		if err != nil {
			return stored, err
		}
		if len(next.Items) == 0 {
			break
		}
		if err := store1(next.Items); err != nil {
			return stored, err
		}
	}
	return stored, nil
}

// BuildBreakoutQuery renders the search query for the rolling window. GitHub's
// created: qualifier is date-granular and inclusive on both ends, so truncating
// `from` to its date makes the fetched set a superset of any timestamp-strict
// window the aggregator later applies — never the other way round.
func BuildBreakoutQuery(minStars int, from, to time.Time) string {
	return fmt.Sprintf("stars:>=%d fork:false created:%s..%s",
		minStars, from.UTC().Format("2006-01-02"), to.UTC().Format("2006-01-02"))
}
