package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/hoveychen/opensource-world/internal/ecosystems"
)

// oldSchema is the repos table as it stood before the eco_* health columns were
// added — i.e. what a database created by an earlier build still has on disk.
const oldSchema = `CREATE TABLE repos (
	repo_id BIGINT PRIMARY KEY, full_name VARCHAR NOT NULL, owner VARCHAR, name VARCHAR,
	description VARCHAR, stars INTEGER, forks INTEGER, open_issues INTEGER, watchers INTEGER,
	language VARCHAR, topics VARCHAR, license VARCHAR, homepage VARCHAR, size_kb INTEGER,
	default_branch VARCHAR, archived BOOLEAN, is_fork BOOLEAN, html_url VARCHAR,
	created_at TIMESTAMP, pushed_at TIMESTAMP, updated_at TIMESTAMP, source_synced_at TIMESTAMP,
	eco_synced_at TIMESTAMP, eco_language VARCHAR, eco_license VARCHAR, eco_topics VARCHAR,
	eco_dependencies VARCHAR
)`

// A database created before the health columns existed must be migrated on
// Open. schema.sql's CREATE TABLE IF NOT EXISTS is a no-op for the existing
// table, so without an explicit ALTER the new columns never appear and
// SetEnrichment fails with "column not found" — the binder error seen against
// the real repos.duckdb.
func TestOpen_MigratesPreExistingDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.duckdb")

	// Seed an old-schema database directly, bypassing Open's current schema.
	raw, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	// Open must migrate the missing health columns onto the existing table.
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open (should migrate): %v", err)
	}
	defer d.Close()

	if err := d.UpsertRepos([]Repo{{ID: 1, FullName: "a/b"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// The operation that failed in production: writing eco_subscribers into a
	// DB that pre-dates the column.
	if err := d.SetEnrichment("a/b", &ecosystems.Repository{Subscribers: 7}); err != nil {
		t.Fatalf("SetEnrichment after migration: %v", err)
	}
}
