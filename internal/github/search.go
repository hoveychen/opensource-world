// Package github is a minimal GitHub Search API client tuned for enumerating
// repositories under the 1000-results-per-query cap and the 30 req/min search
// rate limit.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/hoveychen/opensource-world/internal/db"
)

const (
	apiBase = "https://api.github.com"
	// Search is limited to 30 requests/minute for authenticated users; 2.1s
	// between calls keeps us just under that without relying solely on headers.
	minInterval = 2100 * time.Millisecond
)

// Client talks to the GitHub Search API with one in-flight request at a time.
type Client struct {
	token    string
	http     *http.Client
	lastCall time.Time
}

// NewClient builds a Client with the given token.
func NewClient(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// SearchResult is the subset of the search response we consume.
type SearchResult struct {
	TotalCount        int          `json:"total_count"`
	IncompleteResults bool         `json:"incomplete_results"`
	Items             []SearchItem `json:"items"`
}

// SearchItem maps a repository object from the search response.
type SearchItem struct {
	ID          int64  `json:"id"`
	FullName    string `json:"full_name"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Owner       struct {
		Login string `json:"login"`
	} `json:"owner"`
	StargazersCount int      `json:"stargazers_count"`
	ForksCount      int      `json:"forks_count"`
	OpenIssuesCount int      `json:"open_issues_count"`
	WatchersCount   int      `json:"watchers_count"`
	Language        string   `json:"language"`
	Topics          []string `json:"topics"`
	License         *struct {
		SPDXID string `json:"spdx_id"`
	} `json:"license"`
	Homepage      string    `json:"homepage"`
	Size          int       `json:"size"`
	DefaultBranch string    `json:"default_branch"`
	Archived      bool      `json:"archived"`
	Fork          bool      `json:"fork"`
	HTMLURL       string    `json:"html_url"`
	CreatedAt     time.Time `json:"created_at"`
	PushedAt      time.Time `json:"pushed_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ToRepo converts a search item to the storage struct.
func (it SearchItem) ToRepo() db.Repo {
	r := db.Repo{
		ID:            it.ID,
		FullName:      it.FullName,
		Owner:         it.Owner.Login,
		Name:          it.Name,
		Description:   it.Description,
		Stars:         it.StargazersCount,
		Forks:         it.ForksCount,
		OpenIssues:    it.OpenIssuesCount,
		Watchers:      it.WatchersCount,
		Language:      it.Language,
		Topics:        it.Topics,
		Homepage:      it.Homepage,
		SizeKB:        it.Size,
		DefaultBranch: it.DefaultBranch,
		Archived:      it.Archived,
		Fork:          it.Fork,
		HTMLURL:       it.HTMLURL,
		CreatedAt:     it.CreatedAt,
		PushedAt:      it.PushedAt,
		UpdatedAt:     it.UpdatedAt,
	}
	if it.License != nil {
		r.License = it.License.SPDXID
	}
	return r
}

// SearchRepositories runs one search query for the given page (1-based,
// per_page=100, sorted by stars desc for stable in-window paging).
func (c *Client) SearchRepositories(ctx context.Context, query string, page int) (*SearchResult, error) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("per_page", "100")
	q.Set("page", strconv.Itoa(page))
	q.Set("sort", "stars")
	q.Set("order", "desc")
	endpoint := apiBase + "/search/repositories?" + q.Encode()

	// Up to a few attempts to ride out secondary-rate-limit / 5xx blips.
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		c.throttle()
		res, retry, err := c.do(ctx, endpoint)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if retry <= 0 {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retry):
		}
	}
	return nil, fmt.Errorf("search %q page %d: %w", query, page, lastErr)
}

// throttle enforces the minimum spacing between API calls.
func (c *Client) throttle() {
	if !c.lastCall.IsZero() {
		if wait := minInterval - time.Since(c.lastCall); wait > 0 {
			time.Sleep(wait)
		}
	}
	c.lastCall = time.Now()
}

// do performs a single request. On a rate-limit or transient error it returns a
// positive retry duration; on a fatal error it returns retry<=0.
func (c *Client) do(ctx context.Context, endpoint string) (*SearchResult, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, -1, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "opensource-world-crawler")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 5 * time.Second, err // network blip: retry
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == http.StatusOK:
		var sr SearchResult
		if err := json.Unmarshal(body, &sr); err != nil {
			return nil, -1, fmt.Errorf("decode response: %w", err)
		}
		return &sr, 0, nil
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		// Primary or secondary rate limit. Honor Retry-After, else x-ratelimit-reset.
		return nil, rateLimitWait(resp), fmt.Errorf("rate limited (HTTP %d)", resp.StatusCode)
	case resp.StatusCode >= 500:
		return nil, 5 * time.Second, fmt.Errorf("server error HTTP %d", resp.StatusCode)
	default:
		return nil, -1, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
}

// rateLimitWait computes how long to sleep before retrying a rate-limited call.
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
	return 60 * time.Second // conservative default for secondary limits
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
