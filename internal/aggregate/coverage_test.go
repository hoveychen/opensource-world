package aggregate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoveychen/opensource-world/internal/db"
)

// Coverage is computed by summing each done window's (stars × days) area over a
// cell and dividing by the cell area, which is only correct when windows tile
// the keyspace disjointly. Interior checkpoint windows overlap their children,
// so they must be excluded — otherwise their area double-counts and over-reports
// coverage. Here a leaf covers half a cell (cov 0.5) while an overlapping
// interior window spans the whole cell; the interior one must be ignored.
func TestCoverageExcludesInteriorWindows(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "cov.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	// One repo so the 2020-06 column exists; stars 15 lands in band 10-19.
	created := time.Date(2020, 6, 15, 0, 0, 0, 0, time.UTC)
	if err := d.UpsertRepos([]db.Repo{{ID: 1, FullName: "o/r", Stars: 15, CreatedAt: created}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mStart := time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)
	// Leaf: band 10-19 over the first 15 days of June → half the month → cov 0.5.
	if err := d.MarkWindowDone(10, 19, mStart, mStart.AddDate(0, 0, 14), 100, 100); err != nil {
		t.Fatalf("leaf: %v", err)
	}
	// Interior: same band over the WHOLE month. If it were counted, the summed
	// area would exceed the cell and cov would round up to 1.0.
	if err := d.MarkInteriorDone(10, 19, mStart, mStart.AddDate(0, 0, 29), 5000); err != nil {
		t.Fatalf("interior: %v", err)
	}

	out := t.TempDir()
	if err := writeCoverage(d, out); err != nil {
		t.Fatalf("writeCoverage: %v", err)
	}

	var cd CoverageData
	raw, err := os.ReadFile(filepath.Join(out, "coverage.json"))
	if err != nil {
		t.Fatalf("read coverage.json: %v", err)
	}
	if err := json.Unmarshal(raw, &cd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	band := -1
	for i, b := range cd.Bands {
		if b.Lo == 10 {
			band = i
			break
		}
	}
	col := -1
	for i, m := range cd.Months {
		if m == "2020-06" {
			col = i
			break
		}
	}
	if band < 0 || col < 0 {
		t.Fatalf("cell (band=%d, month=%d) not found", band, col)
	}

	if got := cd.Cells[band][col].Cov; got != 0.5 {
		t.Errorf("cov = %v; want 0.5 (interior window must not inflate it to 1.0)", got)
	}
}
