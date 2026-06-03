package aggregate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hoveychen/opensource-world/internal/db"
	"github.com/hoveychen/opensource-world/internal/ecosystems"
)

func strptr(s string) *string { return &s }

// writeHealth must derive the DDS histogram, file-adoption rates and scorecard
// average from the eco_* columns, counting only enriched rows.
func TestWriteHealth(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "h.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	// Three repos seeded, two enriched (one with a low DDS + scorecard, one with
	// a high DDS, no scorecard), one left un-enriched so it must be excluded.
	if err := d.UpsertRepos([]db.Repo{
		{ID: 1, FullName: "a/lowdds"},
		{ID: 2, FullName: "b/highdds"},
		{ID: 3, FullName: "c/pending"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.SetEnrichment("a/lowdds", &ecosystems.Repository{
		Subscribers: 10,
		CommitStats: &ecosystems.CommitStats{DDS: 0.1},
		Metadata:    &ecosystems.Metadata{Files: map[string]*string{"readme": strptr("README.md"), "security": strptr("SECURITY.md")}},
		Scorecard:   &ecosystems.Scorecard{Data: &ecosystems.ScorecardData{Score: 4.0}},
	}); err != nil {
		t.Fatalf("enrich a: %v", err)
	}
	if err := d.SetEnrichment("b/highdds", &ecosystems.Repository{
		Subscribers: 20,
		CommitStats: &ecosystems.CommitStats{DDS: 0.9},
		Metadata:    &ecosystems.Metadata{Files: map[string]*string{"readme": strptr("README.md")}},
	}); err != nil {
		t.Fatalf("enrich b: %v", err)
	}

	dir := t.TempDir()
	if err := writeHealth(d, dir); err != nil {
		t.Fatalf("writeHealth: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "health.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var h HealthData
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if h.EnrichedStats != 2 {
		t.Errorf("EnrichedStats = %d, want 2 (pending repo excluded)", h.EnrichedStats)
	}
	// DDS 0.1 -> bucket 0 [0,0.2); DDS 0.9 -> bucket 4 [0.8,1.0].
	if h.DDSBuckets[0].Repos != 1 {
		t.Errorf("DDSBuckets[0] = %d, want 1", h.DDSBuckets[0].Repos)
	}
	if h.DDSBuckets[4].Repos != 1 {
		t.Errorf("DDSBuckets[4] = %d, want 1", h.DDSBuckets[4].Repos)
	}
	// readme present in both (rate 1.0), security in one (rate 0.5).
	rate := map[string]float64{}
	present := map[string]int64{}
	for _, f := range h.Files {
		rate[f.Kind] = f.Rate
		present[f.Kind] = f.Present
	}
	if present["readme"] != 2 || rate["readme"] != 1.0 {
		t.Errorf("readme present=%d rate=%v, want 2 / 1.0", present["readme"], rate["readme"])
	}
	if present["security"] != 1 || rate["security"] != 0.5 {
		t.Errorf("security present=%d rate=%v, want 1 / 0.5", present["security"], rate["security"])
	}
	// Files must be sorted most-present first.
	if len(h.Files) > 0 && h.Files[0].Kind != "readme" {
		t.Errorf("Files[0] = %q, want readme (most present)", h.Files[0].Kind)
	}
	// Only a/lowdds had a scorecard (4.0).
	if h.ScorecardCount != 1 || h.ScorecardAvg != 4.0 {
		t.Errorf("scorecard count=%d avg=%v, want 1 / 4.0", h.ScorecardCount, h.ScorecardAvg)
	}
}
