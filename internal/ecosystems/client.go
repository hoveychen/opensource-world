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
	"time"
)

const (
	apiBase = "https://repos.ecosyste.ms/api/v1"
	// Anonymous tier is ~5000 req/hour; ~0.8s spacing stays under it.
	anonInterval = 800 * time.Millisecond
	// Polite tier (joined by passing a mailto) is ~15000 req/hour; ~0.26s
	// spacing (~13.8k/hr) stays comfortably under it.
	politeInterval = 260 * time.Millisecond
)

// Client talks to the ecosyste.ms Repos API, one request at a time.
//
// Passing a non-empty mailto joins the "polite pool": ecosyste.ms raises the
// rate limit from ~5000 to ~15000 req/hour. Verified mechanism (2026-06): only
// the `mailto=` query parameter engages it — putting mailto in the User-Agent
// did NOT (the response stayed tier=anonymous, limit=5000).
type Client struct {
	http     *http.Client
	mailto   string
	interval time.Duration
	lastCall time.Time

	loggedTier bool // log the served rate-limit tier once, for confirmation
}

// NewClient builds an ecosyste.ms client. If mailto is non-empty it is sent as
// the `mailto=` query parameter to join the polite pool.
func NewClient(mailto string) *Client {
	interval := anonInterval
	if mailto != "" {
		interval = politeInterval
	}
	return &Client{
		http:     &http.Client{Timeout: 30 * time.Second},
		mailto:   mailto,
		interval: interval,
	}
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
	for attempt := 0; attempt < 5; attempt++ {
		c.throttle()
		repo, retry, err := c.do(ctx, endpoint, fullName)
		if err == nil {
			return repo, nil
		}
		lastErr = err
		var nf *NotFound
		if asNotFound(err, &nf) || retry <= 0 {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retry):
		}
	}
	return nil, fmt.Errorf("get %q: %w", fullName, lastErr)
}

func (c *Client) throttle() {
	if !c.lastCall.IsZero() {
		if wait := c.interval - time.Since(c.lastCall); wait > 0 {
			time.Sleep(wait)
		}
	}
	c.lastCall = time.Now()
}

func (c *Client) do(ctx context.Context, endpoint, fullName string) (*Repository, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, -1, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "opensource-world-crawler")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 5 * time.Second, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == http.StatusOK:
		if !c.loggedTier {
			c.loggedTier = true
			log.Printf("ecosyste.ms rate-limit tier=%s limit=%s/hr",
				resp.Header.Get("X-RateLimit-Tier"), resp.Header.Get("X-RateLimit-Limit"))
		}
		var r Repository
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, -1, fmt.Errorf("decode %q: %w", fullName, err)
		}
		return &r, 0, nil
	case resp.StatusCode == http.StatusNotFound:
		return nil, -1, &NotFound{FullName: fullName}
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden:
		return nil, rateLimitWait(resp), fmt.Errorf("rate limited (HTTP %d)", resp.StatusCode)
	case resp.StatusCode >= 500:
		return nil, 5 * time.Second, fmt.Errorf("server error HTTP %d", resp.StatusCode)
	default:
		return nil, -1, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
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

func asNotFound(err error, target **NotFound) bool {
	nf, ok := err.(*NotFound)
	if ok {
		*target = nf
	}
	return ok
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return strings.TrimSpace(s)
}
