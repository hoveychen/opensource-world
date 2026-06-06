package github

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoveychen/opensource-world/internal/db"
)

// recordingSearcher records every query and treats every window as a small,
// fully-pageable leaf, so each band is crawled with exactly one query and the
// order of bands is visible in the query log.
type recordingSearcher struct{ queries []string }

func (r *recordingSearcher) SearchRepositories(_ context.Context, query string, _ int) (*SearchResult, error) {
	r.queries = append(r.queries, query)
	return &SearchResult{TotalCount: 1}, nil
}

func starLoOf(t *testing.T, query string) int {
	t.Helper()
	var lo, hi int
	if _, err := fmt.Sscanf(query, "stars:%d..%d", &lo, &hi); err != nil {
		t.Fatalf("parse %q: %v", query, err)
	}
	return lo
}

// The coverage map stayed visually frozen because enumerate drained the huge
// 10-19 band before any higher band started. crawlBands must visit bands
// high-star-first, so the high (sparse) bands light up their rows immediately
// and 10-19 is crawled last.
func TestCrawlBands_HighStarBandsFirst(t *testing.T) {
	rec := &recordingSearcher{}
	d, err := db.Open(filepath.Join(t.TempDir(), "band.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	en := &enumerator{search: rec, store: d}

	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := en.crawlBands(context.Background(), 10, starCeiling, day, day); err != nil {
		t.Fatalf("crawlBands: %v", err)
	}

	if len(rec.queries) != len(starBandBounds) {
		t.Fatalf("got %d queries, want one per band (%d): %v", len(rec.queries), len(starBandBounds), rec.queries)
	}
	// First band crawled must be the top one (5000+).
	if !strings.HasPrefix(rec.queries[0], "stars:5000..") {
		t.Errorf("first query = %q, want the 5000+ band first", rec.queries[0])
	}
	// star lower bounds must be strictly descending (high → low).
	prev := starLoOf(t, rec.queries[0])
	for _, q := range rec.queries[1:] {
		lo := starLoOf(t, q)
		if lo >= prev {
			t.Errorf("band order not strictly descending: %d after %d (%v)", lo, prev, rec.queries)
		}
		prev = lo
	}
	// Last band crawled must be 10-19.
	if last := rec.queries[len(rec.queries)-1]; !strings.HasPrefix(last, "stars:10..19") {
		t.Errorf("last query = %q, want the 10-19 band last", last)
	}
}

func TestStarBandsFor(t *testing.T) {
	cases := []struct {
		name     string
		min, max int
		wantHi   []starBand // expected, high → low
	}{
		{
			name: "full range 10..ceiling",
			min:  10, max: starCeiling,
			wantHi: []starBand{
				{5000, starCeiling}, {1000, 4999}, {500, 999}, {200, 499},
				{100, 199}, {50, 99}, {20, 49}, {10, 19},
			},
		},
		{
			name: "min 1000 drops low bands",
			min:  1000, max: starCeiling,
			wantHi: []starBand{{5000, starCeiling}, {1000, 4999}},
		},
		{
			name: "max clips top band",
			min:  10, max: 300,
			wantHi: []starBand{{200, 300}, {100, 199}, {50, 99}, {20, 49}, {10, 19}},
		},
		{
			name: "partial low band",
			min:  15, max: 49,
			wantHi: []starBand{{20, 49}, {15, 19}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := starBandsFor(c.min, c.max)
			if len(got) != len(c.wantHi) {
				t.Fatalf("got %v, want %v", got, c.wantHi)
			}
			for i := range got {
				if got[i] != c.wantHi[i] {
					t.Errorf("band[%d] = %v, want %v", i, got[i], c.wantHi[i])
				}
			}
		})
	}
}
