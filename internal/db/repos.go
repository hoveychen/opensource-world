package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hoveychen/opensource-world/internal/ecosystems"
)

// Repo is the normalized repository record stored in the repos table.
type Repo struct {
	ID            int64
	FullName      string
	Owner         string
	Name          string
	Description   string
	Stars         int
	Forks         int
	OpenIssues    int
	Watchers      int
	Language      string
	Topics        []string
	License       string
	Homepage      string
	SizeKB        int
	DefaultBranch string
	Archived      bool
	Fork          bool
	HTMLURL       string
	CreatedAt     time.Time
	PushedAt      time.Time
	UpdatedAt     time.Time
}

// UpsertRepos inserts or updates a batch of repos by repo_id. Enrichment columns
// (eco_*) are never touched here so a later enrich pass is not clobbered.
func (d *DB) UpsertRepos(repos []Repo) error {
	if len(repos) == 0 {
		return nil
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO repos (
		repo_id, full_name, owner, name, description, stars, forks, open_issues,
		watchers, language, topics, license, homepage, size_kb, default_branch,
		archived, is_fork, html_url, created_at, pushed_at, updated_at, source_synced_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT (repo_id) DO UPDATE SET
		full_name=excluded.full_name, owner=excluded.owner, name=excluded.name,
		description=excluded.description, stars=excluded.stars, forks=excluded.forks,
		open_issues=excluded.open_issues, watchers=excluded.watchers,
		language=excluded.language, topics=excluded.topics, license=excluded.license,
		homepage=excluded.homepage, size_kb=excluded.size_kb,
		default_branch=excluded.default_branch, archived=excluded.archived,
		is_fork=excluded.is_fork, html_url=excluded.html_url,
		created_at=excluded.created_at, pushed_at=excluded.pushed_at,
		updated_at=excluded.updated_at, source_synced_at=excluded.source_synced_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, r := range repos {
		topics, _ := json.Marshal(r.Topics)
		if _, err := stmt.Exec(
			r.ID, r.FullName, r.Owner, r.Name, r.Description, r.Stars, r.Forks,
			r.OpenIssues, r.Watchers, nullStr(r.Language), string(topics),
			nullStr(r.License), nullStr(r.Homepage), r.SizeKB, r.DefaultBranch,
			r.Archived, r.Fork, r.HTMLURL, tsOrNil(r.CreatedAt), tsOrNil(r.PushedAt),
			tsOrNil(r.UpdatedAt), now,
		); err != nil {
			return fmt.Errorf("upsert repo %d (%s): %w", r.ID, r.FullName, err)
		}
	}
	return tx.Commit()
}

// IsWindowDone reports whether the exact (stars x date) window was fully drained
// on a previous run, so the enumerator can skip re-fetching it.
func (d *DB) IsWindowDone(starLo, starHi int, dateLo, dateHi time.Time) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT count(*) FROM crawl_windows
		WHERE star_min=? AND star_max=? AND date_min=? AND date_max=? AND done_at IS NOT NULL`,
		starLo, starHi, dateLo.Format("2006-01-02"), dateHi.Format("2006-01-02")).Scan(&n)
	return n > 0, err
}

// MarkWindowDone records a fully-drained leaf window for resumability.
func (d *DB) MarkWindowDone(starLo, starHi int, dateLo, dateHi time.Time, totalCount, fetched int) error {
	_, err := d.Exec(`INSERT INTO crawl_windows
		(star_min, star_max, date_min, date_max, total_count, fetched, done_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT (star_min, star_max, date_min, date_max) DO UPDATE SET
			total_count=excluded.total_count, fetched=excluded.fetched, done_at=excluded.done_at`,
		starLo, starHi, dateLo.Format("2006-01-02"), dateHi.Format("2006-01-02"),
		totalCount, fetched, time.Now().UTC())
	return err
}

// PendingEnrichment returns up to limit repo full_names that have not yet been
// enriched from ecosyste.ms (eco_synced_at IS NULL), highest-star first.
func (d *DB) PendingEnrichment(limit int) ([]string, error) {
	rows, err := d.Query(`SELECT full_name FROM repos
		WHERE eco_synced_at IS NULL ORDER BY stars DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// CountPendingEnrichment returns how many repos still need enrichment.
func (d *DB) CountPendingEnrichment() (int64, error) {
	var n int64
	err := d.QueryRow(`SELECT count(*) FROM repos WHERE eco_synced_at IS NULL`).Scan(&n)
	return n, err
}

// SetEnrichment records ecosyste.ms metadata for a repo and stamps eco_synced_at.
// A nil repo (e.g. for a 404 — not present on ecosyste.ms) still marks the repo
// as processed so it is not retried forever, writing NULL for every eco_* value.
// When fields are absent (a repo with no commit_stats or scorecard), the
// corresponding columns are left NULL rather than zeroed.
func (d *DB) SetEnrichment(fullName string, r *ecosystems.Repository) error {
	// Defaults for the nil-repo (404) case: every enrichment column NULL.
	var (
		lang, lic               any = nil, nil
		topics                  any = "null"
		files                   any = "null"
		subs, commits, comtrs   any = nil, nil, nil
		tags, dds, scorecardVal any = nil, nil, nil
	)
	if r != nil {
		t, _ := json.Marshal(r.Topics)
		topics = string(t)
		f, _ := json.Marshal(r.PresentFiles())
		files = string(f)
		lang = nullStr(r.Language)
		lic = nullStr(r.License)
		subs = int64(r.Subscribers)
		tags = int64(r.TagsCount)
		if r.CommitStats != nil {
			commits = int64(r.CommitStats.TotalCommits)
			comtrs = int64(r.CommitStats.TotalCommitters)
			dds = float64(r.CommitStats.DDS)
		}
		if score, ok := r.ScorecardScore(); ok {
			scorecardVal = score
		}
	}
	_, err := d.Exec(`UPDATE repos SET
		eco_language=?, eco_license=?, eco_topics=?,
		eco_subscribers=?, eco_total_commits=?, eco_total_committers=?,
		eco_dds=?, eco_tags_count=?, eco_files=?, eco_scorecard_score=?,
		eco_synced_at=?
		WHERE full_name=?`,
		lang, lic, topics,
		subs, commits, comtrs, dds, tags, files, scorecardVal,
		time.Now().UTC(), fullName)
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func tsOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
