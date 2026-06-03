package db

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/hoveychen/opensource-world/internal/ecosystems"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.UpsertRepos([]Repo{{ID: 1, FullName: "facebook/react"}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	return d
}

func strptr(s string) *string { return &s }

// A full ecosyste.ms repo must land in the right enrichment columns, and the
// files list must contain only the present (non-null) governance files.
func TestSetEnrichment_WritesAllFields(t *testing.T) {
	d := openTestDB(t)
	repo := &ecosystems.Repository{
		Language:    "JavaScript",
		License:     "mit",
		Topics:      []string{"react", "ui"},
		Subscribers: 6627,
		TagsCount:   160,
		CommitStats: &ecosystems.CommitStats{TotalCommits: 17019, TotalCommitters: 1848, DDS: 0.9065},
		Metadata: &ecosystems.Metadata{Files: map[string]*string{
			"readme":  strptr("README.md"),
			"funding": nil,
		}},
		Scorecard: &ecosystems.Scorecard{Data: &ecosystems.ScorecardData{Score: 6.4}},
	}
	if err := d.SetEnrichment("facebook/react", repo); err != nil {
		t.Fatalf("SetEnrichment: %v", err)
	}

	var (
		subs, commits, committers, tags sql.NullInt64
		dds, score                      sql.NullFloat64
		files                           sql.NullString
		synced                          sql.NullTime
	)
	err := d.QueryRow(`SELECT eco_subscribers, eco_total_commits, eco_total_committers,
		eco_tags_count, eco_dds, eco_scorecard_score, eco_files, eco_synced_at
		FROM repos WHERE full_name='facebook/react'`).Scan(
		&subs, &commits, &committers, &tags, &dds, &score, &files, &synced)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if subs.Int64 != 6627 {
		t.Errorf("eco_subscribers = %d, want 6627", subs.Int64)
	}
	if commits.Int64 != 17019 {
		t.Errorf("eco_total_commits = %d, want 17019", commits.Int64)
	}
	if committers.Int64 != 1848 {
		t.Errorf("eco_total_committers = %d, want 1848", committers.Int64)
	}
	if tags.Int64 != 160 {
		t.Errorf("eco_tags_count = %d, want 160", tags.Int64)
	}
	if dds.Float64 < 0.906 || dds.Float64 > 0.907 {
		t.Errorf("eco_dds = %v, want ~0.9065", dds.Float64)
	}
	if score.Float64 != 6.4 {
		t.Errorf("eco_scorecard_score = %v, want 6.4", score.Float64)
	}
	if !synced.Valid {
		t.Error("eco_synced_at should be set")
	}

	var got []string
	if err := json.Unmarshal([]byte(files.String), &got); err != nil {
		t.Fatalf("eco_files not valid JSON (%q): %v", files.String, err)
	}
	if len(got) != 1 || got[0] != "readme" {
		t.Errorf("eco_files = %v, want [readme] (funding was null)", got)
	}
}

// A nil repo (404 on ecosyste.ms) must still stamp eco_synced_at but leave every
// numeric enrichment column NULL — not zero — so "absent" is distinguishable.
func TestSetEnrichment_NilRepoStampsButLeavesNull(t *testing.T) {
	d := openTestDB(t)
	if err := d.SetEnrichment("facebook/react", nil); err != nil {
		t.Fatalf("SetEnrichment(nil): %v", err)
	}

	var (
		subs, commits sql.NullInt64
		score         sql.NullFloat64
		synced        sql.NullTime
	)
	err := d.QueryRow(`SELECT eco_subscribers, eco_total_commits, eco_scorecard_score, eco_synced_at
		FROM repos WHERE full_name='facebook/react'`).Scan(&subs, &commits, &score, &synced)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if subs.Valid || commits.Valid || score.Valid {
		t.Errorf("numeric eco_* should be NULL for a 404 repo, got subs=%v commits=%v score=%v",
			subs, commits, score)
	}
	if !synced.Valid {
		t.Error("eco_synced_at should be stamped even for a 404 repo")
	}
}
