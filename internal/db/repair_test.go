package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedDB(t *testing.T, path string, n int) {
	t.Helper()
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	repos := make([]Repo, n)
	for i := range repos {
		repos[i] = Repo{ID: int64(i + 1), FullName: fmt.Sprintf("o/r%d", i), Description: fmt.Sprintf("repo number %d with some padding text", i), Stars: i}
	}
	if err := d.UpsertRepos(repos); err != nil {
		t.Fatalf("seed repos: %v", err)
	}
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := d.MarkWindowDone(10, 20, day, day, 5, 5); err != nil {
		t.Fatalf("seed window: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestVerify_HealthyDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ok.duckdb")
	seedDB(t, path, 1000)
	if err := Verify(path); err != nil {
		t.Fatalf("verify healthy DB: %v", err)
	}
}

// A file cut short — what a snapshot taken mid-write looks like — must fail
// Verify rather than be uploaded as the new canonical DB.
func TestVerify_TruncatedDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "torn.duckdb")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO repos (repo_id, full_name, description, stars)
		SELECT i, 'o/r' || i, repeat('x', 200) || i, i FROM range(200000) t(i)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	d.Close()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, fi.Size()*2/3); err != nil {
		t.Fatal(err)
	}
	if err := Verify(path); err == nil {
		t.Fatal("verify passed on a truncated DB")
	}
}

// Rebuild must carry every row across even when the source table predates the
// migration columns and so orders them differently from a fresh schema.
func TestRebuild_CopiesAllTables(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.duckdb")
	raw, err := sql.Open("duckdb", src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	seedDB(t, src, 1000)

	dst := filepath.Join(dir, "new.duckdb")
	if err := Rebuild(src, dst); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if err := Verify(dst); err != nil {
		t.Fatalf("verify rebuilt DB: %v", err)
	}
	d, err := Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s, err := d.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalRepos != 1000 || s.WindowsDone != 1 || s.MaxStars.Int64 != 999 {
		t.Fatalf("rebuilt stats = %+v", s)
	}
	var desc string
	if err := d.QueryRow(`SELECT description FROM repos WHERE repo_id = 42`).Scan(&desc); err != nil || desc != "repo number 41 with some padding text" {
		t.Fatalf("description = %q, %v", desc, err)
	}
}
