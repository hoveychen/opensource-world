package ecosystems

import (
	"context"
	"errors"
	"testing"
)

// fakeStore is an in-memory Store: PendingEnrichment returns un-stamped names.
type fakeStore struct {
	pending []string
	stamped map[string]bool
}

func (s *fakeStore) PendingEnrichment(limit int) ([]string, error) {
	var out []string
	for _, n := range s.pending {
		if !s.stamped[n] {
			out = append(out, n)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *fakeStore) SetEnrichment(fullName string, _ *Repository) error {
	s.stamped[fullName] = true
	return nil
}

// fakeFetcher returns a server error for one specific repo, success otherwise.
type fakeFetcher struct{ failOn string }

func (f *fakeFetcher) GetRepository(_ context.Context, fullName string) (*Repository, error) {
	if fullName == f.failOn {
		return nil, errors.New("server error HTTP 500")
	}
	return &Repository{Language: "Go"}, nil
}

// One repo erroring must NOT abort the whole run — the others must still be
// enriched. This reproduces the CI failure where a single HTTP 500 killed enrich.
func TestEnrich_ContinuesPastSingleError(t *testing.T) {
	store := &fakeStore{
		pending: []string{"a/1", "b/2", "c/3", "d/4"},
		stamped: map[string]bool{},
	}
	f := &fakeFetcher{failOn: "b/2"}

	stamped, failed, err := Enrich(context.Background(), store, f, 0)
	if err != nil {
		t.Fatalf("Enrich aborted on a single repo error: %v", err)
	}
	if stamped != 3 {
		t.Errorf("stamped = %d, want 3 (a/1, c/3, d/4)", stamped)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want 1 (b/2)", failed)
	}
	for _, ok := range []string{"a/1", "c/3", "d/4"} {
		if !store.stamped[ok] {
			t.Errorf("%s should have been enriched", ok)
		}
	}
	if store.stamped["b/2"] {
		t.Errorf("b/2 errored; it should stay pending for a later run, not be stamped")
	}
}
