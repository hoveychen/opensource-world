package ecosystems

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeStore is an in-memory Store: PendingEnrichment returns un-stamped names.
// It is mutex-guarded because Enrich now stamps from many worker goroutines.
type fakeStore struct {
	mu      sync.Mutex
	pending []string
	stamped map[string]bool
}

func (s *fakeStore) PendingEnrichment(limit int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stamped[fullName] = true
	return nil
}

func (s *fakeStore) isStamped(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stamped[name]
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
		if !store.isStamped(ok) {
			t.Errorf("%s should have been enriched", ok)
		}
	}
	if store.isStamped("b/2") {
		t.Errorf("b/2 errored; it should stay pending for a later run, not be stamped")
	}
}

// blockingFetcher blocks the named repo until release is closed; all others
// return immediately. It models a repo parked in a long 520 backoff.
type blockingFetcher struct {
	blockOn string
	release chan struct{}
}

func (f *blockingFetcher) GetRepository(ctx context.Context, fullName string) (*Repository, error) {
	if fullName == f.blockOn {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &Repository{Language: "Go"}, nil
}

// The whole point of concurrency: one repo stuck in a long backoff must not
// block the repos behind it. With a serial loop, if the stuck repo is processed
// first the others never start until it finishes; here they must all complete
// while it is still blocked.
func TestEnrich_SlowRepoDoesNotBlockOthers(t *testing.T) {
	fast := []string{"a", "b", "c", "d", "e"}
	store := &fakeStore{
		pending: append([]string{"slow"}, fast...),
		stamped: map[string]bool{},
	}
	f := &blockingFetcher{blockOn: "slow", release: make(chan struct{})}

	done := make(chan struct{})
	go func() {
		Enrich(context.Background(), store, f, 0)
		close(done)
	}()

	// The fast repos must all get stamped while "slow" is still blocked.
	deadline := time.After(2 * time.Second)
	for {
		all := true
		for _, n := range fast {
			if !store.isStamped(n) {
				all = false
				break
			}
		}
		if all {
			break
		}
		select {
		case <-deadline:
			t.Fatal("fast repos did not finish while a slow repo was blocked — enrich is still serial")
		case <-time.After(5 * time.Millisecond):
		}
	}

	close(f.release) // let "slow" finish so Enrich can return
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Enrich did not return after the slow repo was released")
	}
	if !store.isStamped("slow") {
		t.Errorf("slow should have been enriched after release")
	}
}

// Many repos with a few transient failures, exercised concurrently (run under
// -race). Counts must match the serial semantics: every non-failing repo
// stamped, failing ones left pending.
func TestEnrich_ConcurrentCountsAreCorrect(t *testing.T) {
	var pending []string
	for i := 0; i < 60; i++ {
		pending = append(pending, fmt.Sprintf("o/r%d", i))
	}
	store := &fakeStore{pending: pending, stamped: map[string]bool{}}
	f := &multiFailFetcher{failOn: map[string]bool{"o/r3": true, "o/r17": true, "o/r42": true}}

	stamped, failed, err := Enrich(context.Background(), store, f, 0)
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if stamped != 57 {
		t.Errorf("stamped = %d, want 57", stamped)
	}
	if failed != 3 {
		t.Errorf("failed = %d, want 3", failed)
	}
}

type multiFailFetcher struct{ failOn map[string]bool }

func (f *multiFailFetcher) GetRepository(_ context.Context, fullName string) (*Repository, error) {
	if f.failOn[fullName] {
		return nil, errors.New("server error HTTP 520")
	}
	return &Repository{Language: "Go"}, nil
}
