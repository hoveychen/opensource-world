package github

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/hoveychen/opensource-world/internal/db"
)

// resultCap is GitHub Search's hard ceiling: at most 1000 results per query.
const resultCap = 1000

// starCeiling is a fixed upper bound for star bisection. Using a constant (no
// repo has anywhere near 10M stars) keeps window bounds deterministic across
// runs, so the resume check matches exactly. `stars:N..10000000` is equivalent
// to `stars:>=N`.
const starCeiling = 10_000_000

// EnumerateOptions configures the bisection crawl.
type EnumerateOptions struct {
	MinStars int       // lower star bound, inclusive (e.g. 10)
	MaxStars int       // upper star bound; 0 = auto-probe the current max
	From     time.Time // earliest created date to consider
	To       time.Time // latest created date to consider (inclusive)
}

// starBandBounds are the lower edges of the coverage star bands (they mirror the
// heatmap rows: 10-19, 20-49, ... 5000+). Enumerate crawls one band at a time so
// progress spreads across every row of the coverage map instead of piling into
// the single largest band.
var starBandBounds = []int{10, 20, 50, 100, 200, 500, 1000, 5000}

// starBand is one inclusive star range to crawl as a unit.
type starBand struct{ lo, hi int }

// starBandsFor returns the coverage star bands intersected with [minStars,
// maxStars], ordered HIGH stars first. High bands hold very few repos so they
// drain almost instantly and light up their heatmap rows right away; the huge
// 10-19 band is crawled last. A band that falls entirely outside the requested
// range is dropped, and a partially-covered band is clipped to the range.
func starBandsFor(minStars, maxStars int) []starBand {
	var bands []starBand
	for i, lo := range starBandBounds {
		hi := maxStars
		if i+1 < len(starBandBounds) {
			hi = starBandBounds[i+1] - 1
		}
		if lo < minStars {
			lo = minStars
		}
		if hi > maxStars {
			hi = maxStars
		}
		if lo > hi {
			continue // band outside [minStars, maxStars]
		}
		bands = append(bands, starBand{lo, hi})
	}
	for i, j := 0, len(bands)-1; i < j; i, j = i+1, j-1 {
		bands[i], bands[j] = bands[j], bands[i] // high → low
	}
	return bands
}

// Enumerate walks the (stars x created-date) keyspace, recursively bisecting any
// window whose total_count exceeds 1000, until every leaf window can be fully
// paged. Repos are upserted into store; drained leaf windows are recorded for
// resumability. Bands are crawled high-star-first so the coverage heatmap fills
// breadth-first across rows rather than draining one band before the next starts.
func (c *Client) Enumerate(ctx context.Context, store *db.DB, opts EnumerateOptions) error {
	if opts.MaxStars == 0 {
		opts.MaxStars = starCeiling
	}
	if opts.From.IsZero() {
		opts.From = time.Date(2007, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if opts.To.IsZero() {
		opts.To = time.Now().UTC()
	}
	en := &enumerator{search: c, store: store}
	return en.crawlBands(ctx, opts.MinStars, opts.MaxStars, opts.From, opts.To)
}

// crawlBands bisects each coverage star band independently, high band first.
func (en *enumerator) crawlBands(ctx context.Context, minStars, maxStars int, from, to time.Time) error {
	for _, b := range starBandsFor(minStars, maxStars) {
		if err := en.crawl(ctx, b.lo, b.hi, from, to); err != nil {
			return err
		}
	}
	return nil
}

// searcher is the slice of *Client the enumerator needs, extracted so tests can
// drive the bisection with a fake that counts calls instead of hitting GitHub.
type searcher interface {
	SearchRepositories(ctx context.Context, query string, page int) (*SearchResult, error)
}

type enumerator struct {
	search searcher
	store  *db.DB
}

func (en *enumerator) crawl(ctx context.Context, starLo, starHi int, dateLo, dateHi time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Leaf already drained on a previous run?
	if done, err := en.store.IsWindowDone(starLo, starHi, dateLo, dateHi); err != nil {
		return err
	} else if done {
		return nil
	}

	query := buildQuery(starLo, starHi, dateLo, dateHi)
	res, err := en.search.SearchRepositories(ctx, query, 1)
	if err != nil {
		return err
	}
	n := res.TotalCount

	if n == 0 {
		return en.store.MarkWindowDone(starLo, starHi, dateLo, dateHi, 0, 0)
	}
	if n <= resultCap {
		return en.drain(ctx, starLo, starHi, dateLo, dateHi, query, n, res)
	}

	// Window too large: split the dimension that still has room to split.
	if starHi > starLo {
		mid := starLo + (starHi-starLo)/2
		log.Printf("split stars [%d..%d] @%d (n=%d, %s..%s)", starLo, starHi, mid, n,
			dateLo.Format("2006-01-02"), dateHi.Format("2006-01-02"))
		if err := en.crawl(ctx, starLo, mid, dateLo, dateHi); err != nil {
			return err
		}
		if err := en.crawl(ctx, mid+1, starHi, dateLo, dateHi); err != nil {
			return err
		}
		// Both halves fully drained: checkpoint this interior node so a resume
		// skips the whole subtree with one lookup instead of re-counting it.
		return en.store.MarkInteriorDone(starLo, starHi, dateLo, dateHi, n)
	}
	if dateHi.After(dateLo) {
		days := int(dateHi.Sub(dateLo).Hours() / 24)
		midDate := dateLo.AddDate(0, 0, days/2)
		log.Printf("split dates [%s..%s] @%s (stars=%d, n=%d)",
			dateLo.Format("2006-01-02"), dateHi.Format("2006-01-02"),
			midDate.Format("2006-01-02"), starLo, n)
		if err := en.crawl(ctx, starLo, starHi, dateLo, midDate); err != nil {
			return err
		}
		if err := en.crawl(ctx, starLo, starHi, midDate.AddDate(0, 0, 1), dateHi); err != nil {
			return err
		}
		return en.store.MarkInteriorDone(starLo, starHi, dateLo, dateHi, n)
	}

	// Single star value AND single day still exceeds 1000: unsplittable. Drain
	// the accessible 1000 and warn about the truncation.
	log.Printf("WARN: unsplittable window stars=%d date=%s has n=%d > %d; capturing first %d",
		starLo, dateLo.Format("2006-01-02"), n, resultCap, resultCap)
	return en.drain(ctx, starLo, starHi, dateLo, dateHi, query, n, res)
}

// drain pages through an already-counted window (n <= 1000, or a capped leaf),
// upserts every repo, and records the window as done. The page-1 result is
// reused to save one request.
func (en *enumerator) drain(ctx context.Context, starLo, starHi int, dateLo, dateHi time.Time, query string, total int, page1 *SearchResult) error {
	pages := int(math.Ceil(float64(min(total, resultCap)) / 100.0))
	fetched := 0

	store := func(items []SearchItem) error {
		repos := make([]db.Repo, len(items))
		for i, it := range items {
			repos[i] = it.ToRepo()
		}
		fetched += len(repos)
		return en.store.UpsertRepos(repos)
	}

	if err := store(page1.Items); err != nil {
		return err
	}
	for p := 2; p <= pages; p++ {
		res, err := en.search.SearchRepositories(ctx, query, p)
		if err != nil {
			return err
		}
		if err := store(res.Items); err != nil {
			return err
		}
		if len(res.Items) == 0 {
			break
		}
	}
	return en.store.MarkWindowDone(starLo, starHi, dateLo, dateHi, total, fetched)
}

// buildQuery renders the search query for a (stars x date) window.
func buildQuery(starLo, starHi int, dateLo, dateHi time.Time) string {
	return fmt.Sprintf("stars:%d..%d fork:false created:%s..%s",
		starLo, starHi, dateLo.Format("2006-01-02"), dateHi.Format("2006-01-02"))
}
