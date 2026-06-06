// Package ecosystems is a minimal client for the ecosyste.ms Repos API, used to
// enrich repositories already enumerated from GitHub with richer metadata.
package ecosystems

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	apiBase = "https://repos.ecosyste.ms/api/v1"
	// Anonymous tier is ~5000 req/hour; ~0.8s spacing stays under it.
	anonInterval = 800 * time.Millisecond
	// Polite tier (joined by passing a mailto) is ~15000 req/hour; ~0.26s
	// spacing (~13.8k/hr) stays comfortably under it.
	politeInterval = 260 * time.Millisecond
	// Retry budget for transient failures. ecosyste.ms sits behind Cloudflare
	// and intermittently 520s (origin overload), so we retry — but with a short
	// exponential backoff (1s, 2s, 4s), not a flat 5s × 5. A repo that keeps
	// failing now costs ~7s worst case instead of ~25s, and one that recovers on
	// the first retry costs ~1s instead of ~5s. 429/403 still honor Retry-After.
	defaultBackoffBase = 1 * time.Second
	defaultMaxAttempts = 4
)

// Client talks to the ecosyste.ms Repos API. It is safe for concurrent use: the
// limiter paces the aggregate request rate while letting many requests be
// in-flight at once, so one repo stuck in a 520 backoff does not stall the rest
// (and request latency stops capping throughput below the rate limit).
//
// Passing a non-empty mailto joins the "polite pool": ecosyste.ms raises the
// rate limit from ~5000 to ~15000 req/hour. Verified mechanism (2026-06): only
// the `mailto=` query parameter engages it — putting mailto in the User-Agent
// did NOT (the response stayed tier=anonymous, limit=5000).
type Client struct {
	http        *http.Client
	mailto      string
	limiter     *rate.Limiter
	backoffBase time.Duration
	maxAttempts int

	tierOnce sync.Once // log the served rate-limit tier once, for confirmation

	// Retry instrumentation. Mutated from many worker goroutines, so all access
	// goes through mu. serverErrors counts EVERY 5xx response, including ones
	// that later recovered on retry — the volume the logs alone could not reveal.
	mu    sync.Mutex
	stats RetryStats
}

// RetryStats is a snapshot of how much retrying the client did — surfaced at the
// end of a run so the 520 volume (otherwise invisible, since recovered retries
// are not logged) and the wall-clock it cost are measurable.
type RetryStats struct {
	ServerErrors  int           // 5xx responses seen (incl. recovered-on-retry)
	RateLimitHits int           // 429/403 responses seen
	Retries       int           // number of backoff sleeps performed
	RetryWait     time.Duration // cumulative time slept waiting to retry
}

// NewClient builds an ecosyste.ms client. If mailto is non-empty it is sent as
// the `mailto=` query parameter to join the polite pool.
func NewClient(mailto string) *Client {
	interval := anonInterval
	if mailto != "" {
		interval = politeInterval
	}
	return &Client{
		http:        &http.Client{Timeout: 30 * time.Second},
		mailto:      mailto,
		limiter:     rate.NewLimiter(rate.Every(interval), 1),
		backoffBase: defaultBackoffBase,
		maxAttempts: defaultMaxAttempts,
	}
}

// RetryStats returns a snapshot of retry counters. Safe to call concurrently.
func (c *Client) RetryStats() RetryStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

func (c *Client) addServerError() { c.mu.Lock(); c.stats.ServerErrors++; c.mu.Unlock() }
func (c *Client) addRateLimit()   { c.mu.Lock(); c.stats.RateLimitHits++; c.mu.Unlock() }
func (c *Client) addRetry(d time.Duration) {
	c.mu.Lock()
	c.stats.Retries++
	c.stats.RetryWait += d
	c.mu.Unlock()
}

// Repository is the subset of the ecosyste.ms repository object we store.
//
// CommitStats / Metadata / Scorecard are pointers because ecosyste.ms omits or
// nulls them for many repos (e.g. a repo with no OSSF scorecard run); a nil
// pointer means "not available", distinct from a zero value.
type Repository struct {
	Language    string       `json:"language"`
	License     string       `json:"license"`
	Topics      []string     `json:"topics"`
	Subscribers int          `json:"subscribers_count"` // true watchers/subscribers (GitHub Search's "watchers" is actually the star count)
	TagsCount   int          `json:"tags_count"`        // number of git tags / releases
	CommitStats *CommitStats `json:"commit_stats"`
	Metadata    *Metadata    `json:"metadata"`
	Scorecard   *Scorecard   `json:"scorecard"`
}

// CommitStats captures the bus-factor signals from ecosyste.ms.
type CommitStats struct {
	TotalCommits    int       `json:"total_commits"`
	TotalCommitters int       `json:"total_committers"`
	DDS             flexFloat `json:"dds"` // developer distribution score: lower = more bus-factor risk
}

// flexFloat parses a float that ecosyste.ms serializes inconsistently — as a
// JSON number for some repos and a quoted string for others (commit_stats.dds
// is "0.245..." for fatedier/frp but 0.9065 for facebook/react). A null or
// empty value decodes to 0.
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexFloat(v)
	return nil
}

// Metadata.Files maps a governance-file kind (readme, contributing, security,
// funding, ...) to its path in the repo, or null when the file is absent.
type Metadata struct {
	Files map[string]*string `json:"files"`
}

// Scorecard wraps the OSSF Scorecard result; only the headline score is stored.
type Scorecard struct {
	Data *ScorecardData `json:"data"`
}

// ScorecardData carries the 0-10 aggregate OSSF Scorecard score.
type ScorecardData struct {
	Score float64 `json:"score"`
}

// PresentFiles returns the sorted kinds of governance files that exist in the
// repo (the keys of Metadata.Files whose value is non-null). Returns nil when
// metadata or its files map is absent.
func (r *Repository) PresentFiles() []string {
	if r.Metadata == nil || len(r.Metadata.Files) == 0 {
		return nil
	}
	var present []string
	for kind, path := range r.Metadata.Files {
		if path != nil && *path != "" {
			present = append(present, kind)
		}
	}
	sort.Strings(present)
	return present
}

// ScorecardScore returns the OSSF Scorecard score and whether it was present.
func (r *Repository) ScorecardScore() (float64, bool) {
	if r.Scorecard == nil || r.Scorecard.Data == nil {
		return 0, false
	}
	return r.Scorecard.Data.Score, true
}

// NotFound reports whether an error from GetRepository was a 404 (repo missing
// from ecosyste.ms), which callers treat as "enriched, nothing to add".
type NotFound struct{ FullName string }

func (e *NotFound) Error() string { return "ecosyste.ms: not found: " + e.FullName }

// GetRepository fetches a repository by "owner/name".
func (c *Client) GetRepository(ctx context.Context, fullName string) (*Repository, error) {
	// Path segment must be URL-escaped: "owner/name" -> "owner%2Fname".
	endpoint := fmt.Sprintf("%s/hosts/GitHub/repositories/%s", apiBase, url.PathEscape(fullName))
	if c.mailto != "" {
		endpoint += "?mailto=" + url.QueryEscape(c.mailto)
	}

	var lastErr error
	backoff := c.backoffBase
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		// Pace the aggregate request rate. Wait blocks only this goroutine until
		// a token is free, so other workers keep the pipeline full meanwhile.
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		repo, retryAfter, retryable, err := c.do(ctx, endpoint, fullName)
		if err == nil {
			return repo, nil
		}
		lastErr = err
		if !retryable {
			return nil, err
		}
		// No point sleeping after the final attempt — we are about to give up.
		if attempt == c.maxAttempts-1 {
			break
		}
		// 429/403 dictate their own wait (Retry-After / reset); transient 5xx
		// and network blips use exponential backoff.
		wait := retryAfter
		if wait <= 0 {
			wait = backoff
			backoff *= 2
		}
		c.addRetry(wait)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, fmt.Errorf("get %q: %w", fullName, lastErr)
}

// do performs one request. retryable reports whether the caller should retry;
// retryAfter is a caller-honored wait for rate limits (0 means "use backoff").
func (c *Client) do(ctx context.Context, endpoint, fullName string) (repo *Repository, retryAfter time.Duration, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "opensource-world-crawler")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, true, err // network blip: retry with backoff
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == http.StatusOK:
		c.tierOnce.Do(func() {
			log.Printf("ecosyste.ms rate-limit tier=%s limit=%s/hr",
				resp.Header.Get("X-RateLimit-Tier"), resp.Header.Get("X-RateLimit-Limit"))
		})
		var r Repository
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, 0, false, fmt.Errorf("decode %q: %w", fullName, err)
		}
		return &r, 0, false, nil
	case resp.StatusCode == http.StatusNotFound:
		return nil, 0, false, &NotFound{FullName: fullName}
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden:
		c.addRateLimit()
		return nil, rateLimitWait(resp), true, fmt.Errorf("rate limited (HTTP %d)", resp.StatusCode)
	case resp.StatusCode >= 500:
		// Counts every 5xx, including ones that recover on a later attempt —
		// that volume is otherwise invisible (only final failures are logged).
		c.addServerError()
		return nil, 0, true, fmt.Errorf("server error HTTP %d", resp.StatusCode)
	default:
		return nil, 0, false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
}

func rateLimitWait(resp *http.Response) time.Duration {
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
			return time.Duration(secs)*time.Second + time.Second
		}
	}
	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
		if epoch, err := strconv.ParseInt(reset, 10, 64); err == nil {
			if wait := time.Until(time.Unix(epoch, 0)); wait > 0 {
				return wait + time.Second
			}
		}
	}
	return 30 * time.Second
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return strings.TrimSpace(s)
}
