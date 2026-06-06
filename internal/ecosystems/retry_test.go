package ecosystems

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// rtFunc adapts a function to http.RoundTripper.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func resp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// testClient returns a client whose HTTP transport replays the given statuses
// in order (the last is repeated once exhausted), with timing zeroed out so the
// retry loop runs instantly.
func testClient(statuses ...int) *Client {
	i := 0
	c := NewClient("test@example.com")
	c.interval = 0
	c.backoffBase = time.Microsecond
	c.http = &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		s := statuses[len(statuses)-1]
		if i < len(statuses) {
			s = statuses[i]
		}
		i++
		if s == http.StatusOK {
			return resp(s, `{"language":"Go"}`), nil
		}
		return resp(s, "err"), nil
	})}
	return c
}

// A 520 that recovers on retry returns the repo, and every 520 seen — including
// the recovered ones the logs never showed — is counted. This is the visibility
// the instrumentation exists to provide.
func TestGetRepository_RecoveredServerErrorsCounted(t *testing.T) {
	c := testClient(520, 520, http.StatusOK)
	repo, err := c.GetRepository(context.Background(), "o/r")
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if repo == nil || repo.Language != "Go" {
		t.Fatalf("repo = %+v, want parsed", repo)
	}
	rs := c.RetryStats()
	if rs.ServerErrors != 2 {
		t.Errorf("ServerErrors = %d, want 2 (both recovered 520s counted)", rs.ServerErrors)
	}
	if rs.Retries != 2 {
		t.Errorf("Retries = %d, want 2", rs.Retries)
	}
	if rs.RetryWait <= 0 {
		t.Errorf("RetryWait = %v, want > 0", rs.RetryWait)
	}
}

// A persistently-failing repo must give up within maxAttempts (not the old flat
// 5 × 5s), so one bad repo can't stall the enrich loop for 25s.
func TestGetRepository_GivesUpAfterMaxAttempts(t *testing.T) {
	c := testClient(520)
	_, err := c.GetRepository(context.Background(), "o/r")
	if err == nil {
		t.Fatal("expected error after exhausting attempts, got nil")
	}
	rs := c.RetryStats()
	if rs.ServerErrors != c.maxAttempts {
		t.Errorf("ServerErrors = %d, want %d (one per attempt)", rs.ServerErrors, c.maxAttempts)
	}
	// No sleep after the final attempt: retries == attempts - 1.
	if rs.Retries != c.maxAttempts-1 {
		t.Errorf("Retries = %d, want %d (no sleep after last attempt)", rs.Retries, c.maxAttempts-1)
	}
}

// A 404 means "not on ecosyste.ms" — terminal, must not be retried or counted
// as a server error.
func TestGetRepository_NotFoundNotRetried(t *testing.T) {
	c := testClient(http.StatusNotFound)
	_, err := c.GetRepository(context.Background(), "o/gone")
	var nf *NotFound
	if !errors.As(err, &nf) {
		t.Fatalf("err = %v, want *NotFound", err)
	}
	rs := c.RetryStats()
	if rs.ServerErrors != 0 || rs.Retries != 0 {
		t.Errorf("ServerErrors=%d Retries=%d, want 0/0 (404 is terminal)", rs.ServerErrors, rs.Retries)
	}
}
