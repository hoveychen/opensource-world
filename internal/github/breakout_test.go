package github

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoveychen/opensource-world/internal/db"
)

// fakeBreakoutSearcher serves a fixed total across 100-item pages and records
// every query it was asked for, so a test can assert both the window the sweep
// built and how far it paged.
type fakeBreakoutSearcher struct {
	total   int
	queries []string
	pages   []int
}

func (f *fakeBreakoutSearcher) SearchRepositories(_ context.Context, query string, page int) (*SearchResult, error) {
	f.queries = append(f.queries, query)
	f.pages = append(f.pages, page)

	remaining := f.total - (page-1)*100
	if remaining > 100 {
		remaining = 100
	}
	items := make([]SearchItem, 0, max(remaining, 0))
	for i := 0; i < remaining; i++ {
		id := int64((page-1)*100 + i + 1)
		items = append(items, SearchItem{
			ID:        id,
			FullName:  fmt.Sprintf("o/r%d", id),
			Name:      fmt.Sprintf("r%d", id),
			CreatedAt: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		})
	}
	return &SearchResult{TotalCount: f.total, Items: items}, nil
}

func newBreakoutStore(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "breakout.duckdb"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func countRepos(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM repos`).Scan(&n); err != nil {
		t.Fatalf("count repos: %v", err)
	}
	return n
}

// The sweep must ask GitHub for exactly the trailing 7-day window at the
// watchlist floor, and page through every result.
func TestFetchBreakouts_WindowQueryAndPaging(t *testing.T) {
	f := &fakeBreakoutSearcher{total: 250}
	store := newBreakoutStore(t)
	now := time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)

	stored, err := fetchBreakouts(context.Background(), f, store, BreakoutOptions{Now: now})
	if err != nil {
		t.Fatalf("fetchBreakouts: %v", err)
	}

	want := "stars:>=300 fork:false created:2026-08-08..2026-08-15"
	if f.queries[0] != want {
		t.Errorf("query = %q, want %q", f.queries[0], want)
	}
	for _, q := range f.queries {
		if q != want {
			t.Errorf("paging changed the query to %q; every page must reuse %q", q, want)
		}
	}
	if got := len(f.pages); got != 3 {
		t.Errorf("paged %d times for 250 results, want 3", got)
	}
	if stored != 250 {
		t.Errorf("stored = %d, want 250", stored)
	}
	if got := countRepos(t, store); got != 250 {
		t.Errorf("repos in db = %d, want 250", got)
	}
}

// Search hard-caps paging at 1000 results. The sweep must stop there instead of
// requesting pages GitHub will reject.
func TestFetchBreakouts_StopsAtResultCap(t *testing.T) {
	f := &fakeBreakoutSearcher{total: 2500}
	store := newBreakoutStore(t)

	stored, err := fetchBreakouts(context.Background(), f, store, BreakoutOptions{})
	if err != nil {
		t.Fatalf("fetchBreakouts: %v", err)
	}
	if stored != resultCap {
		t.Errorf("stored = %d, want the %d cap", stored, resultCap)
	}
	if got := len(f.pages); got != 10 {
		t.Errorf("paged %d times, want 10 (the cap / 100 per page)", got)
	}
}

// An empty window must cost exactly one call and store nothing.
func TestFetchBreakouts_EmptyWindow(t *testing.T) {
	f := &fakeBreakoutSearcher{total: 0}
	store := newBreakoutStore(t)

	stored, err := fetchBreakouts(context.Background(), f, store, BreakoutOptions{})
	if err != nil {
		t.Fatalf("fetchBreakouts: %v", err)
	}
	if stored != 0 {
		t.Errorf("stored = %d, want 0", stored)
	}
	if len(f.pages) != 1 {
		t.Errorf("made %d calls for an empty window, want 1", len(f.pages))
	}
}

// The fetched set must be a superset of any timestamp-strict window applied
// later: truncating `from` to its calendar date can only widen the window.
func TestBuildBreakoutQuery_TruncatesFromToDate(t *testing.T) {
	from := time.Date(2026, 8, 8, 23, 59, 0, 0, time.UTC)
	to := time.Date(2026, 8, 15, 0, 1, 0, 0, time.UTC)
	got := BuildBreakoutQuery(1000, from, to)
	want := "stars:>=1000 fork:false created:2026-08-08..2026-08-15"
	if got != want {
		t.Errorf("BuildBreakoutQuery = %q, want %q", got, want)
	}
}
