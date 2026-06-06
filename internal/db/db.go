// Package db wraps the DuckDB database used to store crawled repositories.
package db

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "github.com/marcboeker/go-duckdb/v2"
)

//go:embed schema.sql
var schemaSQL string

// DB is a thin wrapper over *sql.DB bound to a DuckDB file.
type DB struct {
	*sql.DB
}

// Open opens (creating if necessary) the DuckDB file at path and applies the
// schema. The schema uses CREATE TABLE IF NOT EXISTS so Open is idempotent.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open duckdb %q: %w", path, err)
	}
	// DuckDB is single-writer; one connection avoids lock contention.
	sqlDB.SetMaxOpenConns(1)
	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	for _, stmt := range migrations {
		if _, err := sqlDB.Exec(stmt); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("migrate (%s): %w", stmt, err)
		}
	}
	return &DB{sqlDB}, nil
}

// migrations bring a pre-existing repos table up to the current schema.
// schema.sql's CREATE TABLE IF NOT EXISTS only builds a fresh table, so a
// database created by an earlier build is missing any column added since. Each
// ALTER ... ADD COLUMN IF NOT EXISTS is a no-op when the column already exists,
// making this safe to run on every Open (fresh DBs included).
var migrations = []string{
	`ALTER TABLE repos ADD COLUMN IF NOT EXISTS eco_subscribers INTEGER`,
	`ALTER TABLE repos ADD COLUMN IF NOT EXISTS eco_total_commits INTEGER`,
	`ALTER TABLE repos ADD COLUMN IF NOT EXISTS eco_total_committers INTEGER`,
	`ALTER TABLE repos ADD COLUMN IF NOT EXISTS eco_dds DOUBLE`,
	`ALTER TABLE repos ADD COLUMN IF NOT EXISTS eco_tags_count INTEGER`,
	`ALTER TABLE repos ADD COLUMN IF NOT EXISTS eco_files VARCHAR`,
	`ALTER TABLE repos ADD COLUMN IF NOT EXISTS eco_scorecard_score DOUBLE`,
	// Interior-window checkpoints (resume short-circuit). Pre-existing windows
	// are all leaves, so the FALSE default keeps them counted in coverage/stats.
	`ALTER TABLE crawl_windows ADD COLUMN IF NOT EXISTS interior BOOLEAN DEFAULT FALSE`,
}

// Stats summarizes the contents of the database.
type Stats struct {
	TotalRepos  int64
	Enriched    int64
	Forks       int64 // rows flagged is_fork — should always be 0 (we crawl fork:false)
	WindowsDone int64
	MaxStars    sql.NullInt64
	MinStars    sql.NullInt64
}

// Stats returns a summary of stored repositories.
func (d *DB) Stats() (Stats, error) {
	var s Stats
	row := d.QueryRow(`SELECT
		count(*),
		count(eco_synced_at),
		coalesce(sum(CASE WHEN is_fork THEN 1 ELSE 0 END), 0),
		coalesce(max(stars), 0),
		coalesce(min(stars), 0)
		FROM repos`)
	if err := row.Scan(&s.TotalRepos, &s.Enriched, &s.Forks, &s.MaxStars, &s.MinStars); err != nil {
		return s, fmt.Errorf("scan repo stats: %w", err)
	}
	// Leaf windows only: interior checkpoints overlap their children and would
	// inflate the count (interior IS NOT TRUE also matches legacy NULL rows).
	if err := d.QueryRow(`SELECT count(*) FROM crawl_windows WHERE done_at IS NOT NULL AND interior IS NOT TRUE`).Scan(&s.WindowsDone); err != nil {
		return s, fmt.Errorf("scan window stats: %w", err)
	}
	return s, nil
}
