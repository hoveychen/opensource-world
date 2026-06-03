// Package aggregate computes small JSON summaries from the repos DuckDB for the
// static visualization site (so the browser never loads the full ~GB database).
package aggregate

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/hoveychen/opensource-world/internal/db"
)

// Run writes meta.json, top_repos.json, trends.json, topics.json,
// languages.json and coverage.json into outDir.
func Run(database *db.DB, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := writeMeta(database, outDir); err != nil {
		return fmt.Errorf("meta: %w", err)
	}
	if err := writeTopRepos(database, outDir, 1000); err != nil {
		return fmt.Errorf("top_repos: %w", err)
	}
	if err := writeTrends(database, outDir); err != nil {
		return fmt.Errorf("trends: %w", err)
	}
	if err := writeTopics(database, outDir, 100); err != nil {
		return fmt.Errorf("topics: %w", err)
	}
	if err := writeLanguages(database, outDir, 40); err != nil {
		return fmt.Errorf("languages: %w", err)
	}
	if err := writeCoverage(database, outDir); err != nil {
		return fmt.Errorf("coverage: %w", err)
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

// TopRepo is one row of the star ranking. Topics are included so the frontend
// can filter/search the ranking client-side without another request.
type TopRepo struct {
	FullName    string   `json:"full_name"`
	Stars       int      `json:"stars"`
	Forks       int      `json:"forks"`
	Language    string   `json:"language"`
	Description string   `json:"description"`
	HTMLURL     string   `json:"html_url"`
	Topics      []string `json:"topics"`
}

func writeTopRepos(d *db.DB, outDir string, limit int) error {
	rows, err := d.Query(`SELECT full_name, stars, forks,
		coalesce(language,''), coalesce(description,''), coalesce(html_url,''),
		coalesce(topics,'[]')
		FROM repos ORDER BY stars DESC LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	var out []TopRepo
	for rows.Next() {
		var r TopRepo
		var rawTopics string
		if err := rows.Scan(&r.FullName, &r.Stars, &r.Forks, &r.Language, &r.Description, &r.HTMLURL, &rawTopics); err != nil {
			return err
		}
		if json.Unmarshal([]byte(rawTopics), &r.Topics) != nil || r.Topics == nil {
			r.Topics = []string{}
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return writeJSON(outDir, "top_repos.json", out)
}

// LanguageCount is repos/stars for a programming language.
type LanguageCount struct {
	Language string `json:"language"`
	Repos    int64  `json:"repos"`
	Stars    int64  `json:"stars"`
}

func writeLanguages(d *db.DB, outDir string, limit int) error {
	rows, err := d.Query(`SELECT language, count(*), coalesce(sum(stars),0)
		FROM repos WHERE language IS NOT NULL AND language <> ''
		GROUP BY language ORDER BY count(*) DESC LIMIT ?`, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	var out []LanguageCount
	for rows.Next() {
		var l LanguageCount
		if err := rows.Scan(&l.Language, &l.Repos, &l.Stars); err != nil {
			return err
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return writeJSON(outDir, "languages.json", out)
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

// ---------- crawl coverage heatmap ----------

// coverageStarCeiling caps the open-ended top band for finite area math. It
// matches the enumerator's starCeiling so a fully-drained `stars:>=N` window
// (recorded as star_max = ceiling) lines up exactly with the top band's area.
const coverageStarCeiling = 10_000_000

// coverageBand is one (inclusive) star band — a row of the heatmap.
type coverageBand struct {
	Lo    int    `json:"lo"`
	Hi    int    `json:"hi"`
	Label string `json:"label"`
}

// coverageBands are fixed log-ish star buckets, low to high. The top band is
// open-ended ("5000+"); its Hi is the ceiling used only for area math.
var coverageBands = []coverageBand{
	{10, 19, "10–19"},
	{20, 49, "20–49"},
	{50, 99, "50–99"},
	{100, 199, "100–199"},
	{200, 499, "200–499"},
	{500, 999, "500–999"},
	{1000, 4999, "1k–5k"},
	{5000, coverageStarCeiling, "5k+"},
}

// CoverageCell is one (band x month) tile: the exact repo count discovered
// there plus how much of the keyspace region has been drained (cov in 0..1).
type CoverageCell struct {
	Repos int     `json:"repos"`
	Cov   float64 `json:"cov"`
}

// CoverageData is the heatmap payload: row bands, column months ("2006-01"),
// and a cells[band][month] matrix.
type CoverageData struct {
	Bands  []coverageBand   `json:"bands"`
	Months []string         `json:"months"`
	Cells  [][]CoverageCell `json:"cells"`
}

// writeCoverage builds coverage.json: a (star band x creation month) grid where
// each cell carries the exact repo density (from the repos table) and a crawl
// coverage ratio (from drained crawl_windows, by area overlap). The repo count
// answers "where do repos cluster"; cov distinguishes "crawled but empty" from
// "not yet crawled".
func writeCoverage(d *db.DB, outDir string) error {
	months, monthIdx, err := coverageMonths(d)
	if err != nil {
		return err
	}

	cells := make([][]CoverageCell, len(coverageBands))
	for b := range cells {
		cells[b] = make([]CoverageCell, len(months))
	}

	if err := fillCoverageRepos(d, monthIdx, cells); err != nil {
		return err
	}
	if err := fillCoverageWindows(d, months, cells); err != nil {
		return err
	}

	return writeJSON(outDir, "coverage.json", CoverageData{
		Bands:  coverageBands,
		Months: months,
		Cells:  cells,
	})
}

// coverageMonths returns the column axis: every "YYYY-MM" from 2007-01 through
// the month of the newest repo (or the current month if the DB is empty), plus
// a lookup from label to column index.
func coverageMonths(d *db.DB) ([]string, map[string]int, error) {
	start := time.Date(2007, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Now().UTC()
	var maxCreated sql.NullTime
	if err := d.QueryRow(`SELECT max(created_at) FROM repos WHERE created_at IS NOT NULL`).Scan(&maxCreated); err != nil {
		return nil, nil, err
	}
	if maxCreated.Valid && maxCreated.Time.After(end) {
		end = maxCreated.Time.UTC()
	}

	var months []string
	idx := map[string]int{}
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 1, 0) {
		label := cur.Format("2006-01")
		idx[label] = len(months)
		months = append(months, label)
	}
	return months, idx, nil
}

// fillCoverageRepos tallies the exact repo count per (band x month) from the
// repos table. Repos with stars below the lowest band are ignored.
func fillCoverageRepos(d *db.DB, monthIdx map[string]int, cells [][]CoverageCell) error {
	rows, err := d.Query(`SELECT stars, strftime(created_at, '%Y-%m') AS ym, count(*)
		FROM repos
		WHERE created_at IS NOT NULL AND stars >= ?
		GROUP BY stars, ym`, coverageBands[0].Lo)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var stars int
		var ym string
		var n int
		if err := rows.Scan(&stars, &ym, &n); err != nil {
			return err
		}
		col, ok := monthIdx[ym]
		if !ok {
			continue
		}
		cells[bandOf(stars)][col].Repos += n
	}
	return rows.Err()
}

// bandOf maps a star count to its band index (stars are guaranteed >= bands[0].Lo).
func bandOf(stars int) int {
	for i, b := range coverageBands {
		if stars <= b.Hi {
			return i
		}
	}
	return len(coverageBands) - 1
}

// fillCoverageWindows computes each cell's drained ratio (cov in 0..1) from the
// done crawl_windows, by summing the (stars x days) area each window contributes
// to the cell and dividing by the cell's total area. Windows tile the crawled
// keyspace disjointly, so the summed overlap equals the covered fraction.
func fillCoverageWindows(d *db.DB, months []string, cells [][]CoverageCell) error {
	rows, err := d.Query(`SELECT star_min, star_max, date_min, date_max
		FROM crawl_windows WHERE done_at IS NOT NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()

	area := make([][]float64, len(coverageBands))
	for b := range area {
		area[b] = make([]float64, len(months))
	}

	for rows.Next() {
		var sLo, sHi int
		var dLo, dHi time.Time
		if err := rows.Scan(&sLo, &sHi, &dLo, &dHi); err != nil {
			return err
		}
		for b, band := range coverageBands {
			starOv := overlapInts(sLo, sHi, band.Lo, band.Hi)
			if starOv == 0 {
				continue
			}
			for c, m := range months {
				mStart, _ := time.Parse("2006-01", m)
				mEnd := mStart.AddDate(0, 1, -1)
				dayOv := overlapDays(dLo, dHi, mStart, mEnd)
				if dayOv == 0 {
					continue
				}
				area[b][c] += float64(starOv) * float64(dayOv)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for b, band := range coverageBands {
		for c, m := range months {
			mStart, _ := time.Parse("2006-01", m)
			mEnd := mStart.AddDate(0, 1, -1)
			cellArea := float64(band.Hi-band.Lo+1) * float64(daysInclusive(mStart, mEnd))
			cov := 0.0
			if cellArea > 0 {
				cov = area[b][c] / cellArea
			}
			if cov > 1 {
				cov = 1
			}
			cells[b][c].Cov = math.Round(cov*100) / 100
		}
	}
	return nil
}

// overlapInts returns the count of integers in [aLo,aHi] ∩ [bLo,bHi] (inclusive).
func overlapInts(aLo, aHi, bLo, bHi int) int {
	lo, hi := max(aLo, bLo), min(aHi, bHi)
	if hi < lo {
		return 0
	}
	return hi - lo + 1
}

// overlapDays returns the inclusive day count of [aLo,aHi] ∩ [bLo,bHi].
func overlapDays(aLo, aHi, bLo, bHi time.Time) int {
	lo, hi := aLo, aHi
	if bLo.After(lo) {
		lo = bLo
	}
	if bHi.Before(hi) {
		hi = bHi
	}
	if hi.Before(lo) {
		return 0
	}
	return daysInclusive(lo, hi)
}

func daysInclusive(lo, hi time.Time) int {
	return int(hi.Sub(lo).Hours()/24) + 1
}
