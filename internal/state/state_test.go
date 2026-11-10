package state

import (
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	ok := &Portfolio{Projects: []*Project{
		{Idea: "00001-a", Repo: "o/a", Status: Shipped, Started: "2026-11-01", URL: "https://a"},
		{Idea: "00002-b", Repo: "o/b", Status: Building, Started: "2026-11-02"},
	}}
	if errs := ok.Validate(); len(errs) != 0 {
		t.Fatal(errs)
	}
	bad := &Portfolio{Projects: []*Project{
		{Idea: "00001-a", Repo: "o/a", Status: Building, Started: "2026-11-01"},
		{Idea: "00001-a", Repo: "o/a", Status: "weird", Started: "11/01"},
		{Idea: "00003-c", Status: Iterating, Started: "2026-11-01"},
		{Idea: "00004-d", Status: Shipped, Started: "2026-11-01"},
	}}
	// 作りかけ 3 つ・idea 重複・repo 重複・status 不正・日付不正・shipped なのに url なし
	if errs := bad.Validate(); len(errs) < 6 {
		t.Fatalf("見逃し: %v", errs)
	}
}

func TestActiveAndTried(t *testing.T) {
	pf := &Portfolio{Projects: []*Project{
		{Idea: "a", Status: Dropped}, {Idea: "b", Status: Evaluating, Repo: "o/b"},
	}}
	if pf.Active() == nil || pf.Active().Idea != "b" {
		t.Fatal("active")
	}
	if !pf.Tried()["a"] || !pf.Repos()["o/b"] {
		t.Fatal("tried / repos")
	}
}

func TestRuntimeResetsDaily(t *testing.T) {
	dir := t.TempDir()
	d1 := time.Date(2026, 11, 10, 23, 0, 0, 0, time.Local)
	r, err := LoadRuntime(dir, d1)
	if err != nil {
		t.Fatal(err)
	}
	r.TicksToday, r.ConsecutiveFailures, r.RunWorktree, r.RunSteps = 5, 2, "/wt", 3
	if err := SaveRuntime(dir, r); err != nil {
		t.Fatal(err)
	}
	r2, _ := LoadRuntime(dir, d1.Add(time.Hour)) // 翌日
	if r2.TicksToday != 0 || r2.ConsecutiveFailures != 2 || r2.RunWorktree != "" || r2.RunSteps != 0 {
		t.Fatalf("日付が変わったら tick 数と実行の段の数だけ 0 に: %+v", r2)
	}
	r3, _ := LoadRuntime(dir, d1)
	if r3.TicksToday != 5 || r3.RunWorktree != "/wt" || r3.RunSteps != 3 {
		t.Fatalf("同じ日は持ち越す: %+v", r3)
	}
}

func TestTicksRoundTrip(t *testing.T) {
	root := t.TempDir()
	now := time.Now().Truncate(time.Second)
	for _, r := range []string{"ok", "fail"} {
		if err := AppendTick(root, Tick{Start: now, Phase: PhaseBuild, Result: r}); err != nil {
			t.Fatal(err)
		}
	}
	ts, err := ReadTicks(root)
	if err != nil || len(ts) != 2 || ts[1].Result != "fail" {
		t.Fatal(ts, err)
	}
}

func TestDaysSince(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.Local)
	if DaysSince("2026-11-03", now) != 7 || DaysSince("", now) < 1000 || DaysSince("bad", now) < 1000 {
		t.Fatal("DaysSince")
	}
}
