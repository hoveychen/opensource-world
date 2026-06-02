// Package ecosystems is a minimal client for the ecosyste.ms Repos API, used to
// enrich repositories already enumerated from GitHub with richer metadata.
package ecosystems

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	apiBase = "https://repos.ecosyste.ms/api/v1"
	// ecosyste.ms allows ~5000 req/hour anonymously; ~0.8s spacing stays under it.
	minInterval = 800 * time.Millisecond
)

// Client talks to the ecosyste.ms Repos API, one request at a time.
type Client struct {
	http     *http.Client
	lastCall time.Time
}

// NewClient builds an ecosyste.ms client.
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 30 * time.Second}}
}

// Repository is the subset of the ecosyste.ms repository object we store.
type Repository struct {
	Language string   `json:"language"`
	License  string   `json:"license"`
	Topics   []string `json:"topics"`
}

// NotFound reports whether an error from GetRepository was a 404 (repo missing
// from ecosyste.ms), which callers treat as "enriched, nothing to add".
type NotFound struct{ FullName string }

func (e *NotFound) Error() string { return "ecosyste.ms: not found: " + e.FullName }

// GetRepository fetches a repository by "owner/name".
func (c *Client) GetRepository(ctx context.Context, fullName string) (*Repository, error) {
	// Path segment must be URL-escaped: "owner/name" -> "owner%2Fname".
	endpoint := fmt.Sprintf("%s/hosts/GitHub/repositories/%s", apiBase, url.PathEscape(fullName))

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
		if wait := minInterval - time.Since(c.lastCall); wait > 0 {
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
