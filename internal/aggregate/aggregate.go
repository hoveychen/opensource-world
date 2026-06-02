// Package aggregate computes small JSON summaries from the repos DuckDB for the
// static visualization site (so the browser never loads the full ~GB database).
package aggregate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/hoveychen/opensource-world/internal/db"
)

// Run writes meta.json, top_repos.json, trends.json and topics.json into outDir.
func Run(database *db.DB, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := writeMeta(database, outDir); err != nil {
		return fmt.Errorf("meta: %w", err)
	}
	if err := writeTopRepos(database, outDir, 100); err != nil {
		return fmt.Errorf("top_repos: %w", err)
	}
	if err := writeTrends(database, outDir); err != nil {
		return fmt.Errorf("trends: %w", err)
	}
	if err := writeTopics(database, outDir, 100); err != nil {
		return fmt.Errorf("topics: %w", err)
	}
	return nil
}

func writeJSON(outDir, name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, name), b, 0o644)
}

func writeMeta(d *db.DB, outDir string) error {
	s, err := d.Stats()
	if err != nil {
		return err
	}
	return writeJSON(outDir, "meta.json", map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"total_repos":  s.TotalRepos,
		"enriched":     s.Enriched,
		"min_stars":    s.MinStars.Int64,
		"max_stars":    s.MaxStars.Int64,
	})
}

// TopRepo is one row of the star ranking.
type TopRepo struct {
	FullName    string `json:"full_name"`
	Stars       int    `json:"stars"`
	Forks       int    `json:"forks"`
	Language    string `json:"language"`
	Description string `json:"description"`
	HTMLURL     string `json:"html_url"`
}

func writeTopRepos(d *db.DB, outDir string, limit int) error {
	rows, err := d.Query(`SELECT full_name, stars, forks,
		coalesce(language,''), coalesce(description,''), coalesce(html_url,'')
		FROM repos ORDER BY stars DESC LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	var out []TopRepo
	for rows.Next() {
		var r TopRepo
		if err := rows.Scan(&r.FullName, &r.Stars, &r.Forks, &r.Language, &r.Description, &r.HTMLURL); err != nil {
			return err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return writeJSON(outDir, "top_repos.json", out)
}

// YearBucket is repos/stars created in a given year.
type YearBucket struct {
	Year  int   `json:"year"`
	Repos int64 `json:"repos"`
	Stars int64 `json:"stars"`
}

func writeTrends(d *db.DB, outDir string) error {
	rows, err := d.Query(`SELECT CAST(year(created_at) AS INTEGER) AS y,
		count(*), coalesce(sum(stars),0)
		FROM repos WHERE created_at IS NOT NULL
		GROUP BY y ORDER BY y`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var out []YearBucket
	for rows.Next() {
		var b YearBucket
		if err := rows.Scan(&b.Year, &b.Repos, &b.Stars); err != nil {
			return err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return writeJSON(outDir, "trends.json", out)
}

// TopicCount is the frequency and total stars for a GitHub topic.
type TopicCount struct {
	Topic string `json:"topic"`
	Repos int64  `json:"repos"`
	Stars int64  `json:"stars"`
}

// writeTopics explodes the JSON topics array in Go (robust against DuckDB JSON
// function differences) and tallies frequency + total stars per topic.
func writeTopics(d *db.DB, outDir string, limit int) error {
	rows, err := d.Query(`SELECT topics, stars FROM repos
		WHERE topics IS NOT NULL AND topics <> '[]' AND topics <> ''`)
	if err != nil {
		return err
	}
	defer rows.Close()

	repoCount := map[string]int64{}
	starSum := map[string]int64{}
	for rows.Next() {
		var raw string
		var stars int64
		if err := rows.Scan(&raw, &stars); err != nil {
			return err
		}
		var topics []string
		if json.Unmarshal([]byte(raw), &topics) != nil {
			continue
		}
		for _, t := range topics {
			if t == "" {
				continue
			}
			repoCount[t]++
			starSum[t] += stars
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	out := make([]TopicCount, 0, len(repoCount))
	for t, c := range repoCount {
		out = append(out, TopicCount{Topic: t, Repos: c, Stars: starSum[t]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Repos != out[j].Repos {
			return out[i].Repos > out[j].Repos
		}
		return out[i].Topic < out[j].Topic
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return writeJSON(outDir, "topics.json", out)
}
