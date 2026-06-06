package github

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoveychen/opensource-world/internal/db"
)

// fakeSearcher drives the bisection without touching GitHub. Any star range
// wider than one value reports >1000 (forcing a split); a single star value
// reports a small, fully-pageable count (a leaf). It counts every call so a
// test can assert how much work a resume re-does.
type fakeSearcher struct {
	calls int
}

func (f *fakeSearcher) SearchRepositories(_ context.Context, query string, _ int) (*SearchResult, error) {
	f.calls++
	var lo, hi int
	if _, err := fmt.Sscanf(query, "stars:%d..%d", &lo, &hi); err != nil {
		return nil, fmt.Errorf("parse %q: %w", query, err)
	}
	if hi > lo {
		return &SearchResult{TotalCount: 5000}, nil // too big: must split
	}
	// Single star value: a small leaf with one repo so drain stores something.
	return &SearchResult{
		TotalCount: 50,
		Items:      []SearchItem{{ID: int64(lo), FullName: fmt.Sprintf("o/r%d", lo)}},
	}, nil
}

func newTestEnumerator(t *testing.T, f *fakeSearcher) *enumerator {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "enum.duckdb"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return &enumerator{search: f, store: d}
}

// Once every window in a subtree is drained, a re-run must skip the entire
// subtree with no API calls. This is the resume short-circuit: interior
// bisection nodes are checkpointed, so a resume hits IsWindowDone at the root
// instead of re-counting every split. Without interior checkpointing the
// second pass re-issues a count for each interior node.
func TestEnumerate_ResumeSkipsCompletedSubtree(t *testing.T) {
	f := &fakeSearcher{}
	en := newTestEnumerator(t, f)
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()

	if err := en.crawl(ctx, 10, 13, day, day); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if f.calls == 0 {
		t.Fatalf("first pass made no API calls; fake not wired up")
	}

	f.calls = 0
	if err := en.crawl(ctx, 10, 13, day, day); err != nil {
		t.Fatalf("resume pass: %v", err)
	}
	if f.calls != 0 {
		t.Errorf("resume re-issued %d API calls; a fully-drained tree must short-circuit to 0", f.calls)
	}
}
