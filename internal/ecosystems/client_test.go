package ecosystems

import (
	"encoding/json"
	"testing"
)

// Trimmed but faithful sample of a real ecosyste.ms repository response
// (facebook/react, fetched 2026-06). Exercises every nested field we parse.
const sampleRepoJSON = `{
  "language": "JavaScript",
  "license": "mit",
  "topics": ["react", "ui"],
  "subscribers_count": 6627,
  "tags_count": 160,
  "commit_stats": {
    "total_commits": 17019,
    "total_committers": 1848,
    "mean_commits": 9.21,
    "dds": 0.9065162465479758
  },
  "metadata": {
    "files": {
      "readme": "README.md",
      "contributing": "CONTRIBUTING.md",
      "funding": null,
      "security": "SECURITY.md",
      "governance": null
    }
  },
  "scorecard": {
    "data": {
      "score": 6.4
    }
  }
}`

func TestRepository_ParsesNestedFields(t *testing.T) {
	var r Repository
	if err := json.Unmarshal([]byte(sampleRepoJSON), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if r.Subscribers != 6627 {
		t.Errorf("Subscribers = %d, want 6627", r.Subscribers)
	}
	if r.TagsCount != 160 {
		t.Errorf("TagsCount = %d, want 160", r.TagsCount)
	}
	if r.CommitStats == nil {
		t.Fatal("CommitStats = nil, want parsed")
	}
	if r.CommitStats.TotalCommits != 17019 {
		t.Errorf("TotalCommits = %d, want 17019", r.CommitStats.TotalCommits)
	}
	if r.CommitStats.TotalCommitters != 1848 {
		t.Errorf("TotalCommitters = %d, want 1848", r.CommitStats.TotalCommitters)
	}
	if r.CommitStats.DDS < 0.906 || r.CommitStats.DDS > 0.907 {
		t.Errorf("DDS = %v, want ~0.9065", r.CommitStats.DDS)
	}

	// PresentFiles must drop the null entries (funding, governance) and keep the
	// rest, sorted.
	got := r.PresentFiles()
	want := []string{"contributing", "readme", "security"}
	if len(got) != len(want) {
		t.Fatalf("PresentFiles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("PresentFiles[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	score, ok := r.ScorecardScore()
	if !ok {
		t.Fatal("ScorecardScore ok = false, want true")
	}
	if score != 6.4 {
		t.Errorf("ScorecardScore = %v, want 6.4", score)
	}
}

// A repo missing the optional nested objects (common on ecosyste.ms) must parse
// cleanly and report "not available" rather than panicking.
func TestRepository_HandlesMissingNested(t *testing.T) {
	var r Repository
	if err := json.Unmarshal([]byte(`{"language":"Go"}`), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.CommitStats != nil {
		t.Errorf("CommitStats = %+v, want nil", r.CommitStats)
	}
	if got := r.PresentFiles(); got != nil {
		t.Errorf("PresentFiles = %v, want nil", got)
	}
	if score, ok := r.ScorecardScore(); ok {
		t.Errorf("ScorecardScore ok = true (score %v), want false", score)
	}
}

// ecosyste.ms serializes commit_stats.dds as a JSON string for some repos
// (e.g. fatedier/frp returns "0.2458...") and as a number for others
// (facebook/react returns 0.9065). Both must parse. This reproduces the
// "cannot unmarshal string into float64" error seen during a real enrich run.
func TestRepository_ParsesStringDDS(t *testing.T) {
	cases := map[string]string{
		"string": `{"commit_stats":{"total_commits":842,"dds":"0.24584323040380052"}}`,
		"number": `{"commit_stats":{"total_commits":842,"dds":0.24584323040380052}}`,
	}
	for name, js := range cases {
		t.Run(name, func(t *testing.T) {
			var r Repository
			if err := json.Unmarshal([]byte(js), &r); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if r.CommitStats == nil {
				t.Fatal("CommitStats = nil, want parsed")
			}
			if got := float64(r.CommitStats.DDS); got < 0.245 || got > 0.246 {
				t.Errorf("DDS = %v, want ~0.2458", got)
			}
		})
	}
}
