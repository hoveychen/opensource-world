package aggregate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoveychen/opensource-world/internal/db"
	"github.com/hoveychen/opensource-world/internal/github"
)

func readBreakout(t *testing.T, dir string) BreakoutData {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "breakout.json"))
	if err != nil {
		t.Fatalf("read breakout.json: %v", err)
	}
	var b BreakoutData
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return b
}

// The board splits the rolling window into the >=1000 leaderboard and the tier
// still climbing, and drops anything created before the window — a repo that
// took longer than a week to reach 1000 stars did not go 0-to-1000 in a week.
func TestWriteBreakouts_TiersAndRollingWindow(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	if err := d.UpsertRepos([]db.Repo{
		// 2 days old, way past the bar: the headline case.
		{ID: 1, FullName: "deep/rocket", Stars: 38736, CreatedAt: now.AddDate(0, 0, -2)},
		// 6 days old, just over the bar.
		{ID: 2, FullName: "slow/burner", Stars: 1020, CreatedAt: now.AddDate(0, 0, -6)},
		// 3 days old, still climbing: watchlist.
		{ID: 3, FullName: "still/climbing", Stars: 640, CreatedAt: now.AddDate(0, 0, -3)},
		// Below the watch floor: off the board entirely.
		{ID: 4, FullName: "too/small", Stars: 42, CreatedAt: now.AddDate(0, 0, -1)},
		// Huge, but created two months ago — not a 0-to-1000-in-a-week story.
		{ID: 5, FullName: "old/giant", Stars: 90000, CreatedAt: now.AddDate(0, 0, -60)},
		// Exactly at the window edge, one hour inside.
		{ID: 6, FullName: "edge/inside", Stars: 1500, CreatedAt: now.Add(-github.BreakoutWindow + time.Hour)},
		// One hour outside the window: excluded.
		{ID: 7, FullName: "edge/outside", Stars: 1500, CreatedAt: now.Add(-github.BreakoutWindow - time.Hour)},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dir := t.TempDir()
	if err := writeBreakouts(d, dir, now); err != nil {
		t.Fatalf("writeBreakouts: %v", err)
	}
	b := readBreakout(t, dir)

	leaders := make([]string, len(b.Leaders))
	for i, l := range b.Leaders {
		leaders[i] = l.FullName
	}
	want := []string{"deep/rocket", "edge/inside", "slow/burner"} // stars DESC
	if len(leaders) != len(want) {
		t.Fatalf("leaders = %v, want %v", leaders, want)
	}
	for i := range want {
		if leaders[i] != want[i] {
			t.Fatalf("leaders = %v, want %v (stars DESC)", leaders, want)
		}
	}
	if len(b.Watchlist) != 1 || b.Watchlist[0].FullName != "still/climbing" {
		t.Errorf("watchlist = %+v, want just still/climbing", b.Watchlist)
	}
	if b.WindowDays != 7 || b.LeaderStars != LeaderStars || b.WatchStars != github.BreakoutFloor {
		t.Errorf("header = days:%d leader:%d watch:%d, want 7/%d/%d",
			b.WindowDays, b.LeaderStars, b.WatchStars, LeaderStars, github.BreakoutFloor)
	}
}

// Age and velocity are what make the board readable: "38736 stars in 2 days".
func TestWriteBreakouts_AgeAndVelocity(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	if err := d.UpsertRepos([]db.Repo{
		{ID: 1, FullName: "two/days", Stars: 4000, CreatedAt: now.AddDate(0, 0, -2)},
		// Hours old: age must be floored at a day so velocity stays sane.
		{ID: 2, FullName: "hours/old", Stars: 2000, CreatedAt: now.Add(-3 * time.Hour)},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dir := t.TempDir()
	if err := writeBreakouts(d, dir, now); err != nil {
		t.Fatalf("writeBreakouts: %v", err)
	}
	b := readBreakout(t, dir)

	byName := map[string]Breakout{}
	for _, l := range b.Leaders {
		byName[l.FullName] = l
	}
	if got := byName["two/days"]; got.AgeDays != 2 || got.StarsPerDay != 2000 {
		t.Errorf("two/days age=%v velocity=%v, want 2 / 2000", got.AgeDays, got.StarsPerDay)
	}
	if got := byName["hours/old"]; got.AgeDays != 0.1 || got.StarsPerDay != 2000 {
		t.Errorf("hours/old age=%v velocity=%v, want 0.1 / 2000 (age floored at 1 day)",
			got.AgeDays, got.StarsPerDay)
	}
}

// An empty window must still produce a valid file with empty arrays, not null,
// so the frontend can render "nothing broke out this week" without guarding.
func TestWriteBreakouts_EmptyWindowEmitsArrays(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "b.duckdb"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	dir := t.TempDir()
	if err := writeBreakouts(d, dir, time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("writeBreakouts: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "breakout.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var probe struct {
		Leaders   []Breakout `json:"leaders"`
		Watchlist []Breakout `json:"watchlist"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if probe.Leaders == nil || probe.Watchlist == nil {
		t.Errorf("empty window emitted null arrays; want [] so the frontend needs no guard")
	}
}
