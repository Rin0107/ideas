package state

import (
	"strings"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	ok := &Portfolio{Projects: []*Project{
		{Idea: "00001-a", Repo: "o/a", Status: Shipped, Started: "2026-11-01", URL: "https://a"},
		{Idea: "00002-b", Repo: "o/b", Status: Building, Started: "2026-11-02"},
	}}
	if errs := ok.Validate(1); len(errs) != 0 {
		t.Fatal(errs)
	}
	bad := &Portfolio{Projects: []*Project{
		{Idea: "00001-a", Repo: "o/a", Status: Building, Started: "2026-11-01"},
		{Idea: "00001-a", Repo: "o/a", Status: "weird", Started: "11/01"},
		{Idea: "00003-c", Status: Iterating, Started: "2026-11-01"},
		{Idea: "00004-d", Status: Shipped, Started: "2026-11-01"},
	}}
	// 作りかけ 3 つ・idea 重複・repo 重複・status 不正・日付不正・shipped なのに url なし
	if errs := bad.Validate(1); len(errs) < 6 {
		t.Fatalf("見逃し: %v", errs)
	}
}

func TestActiveAndTried(t *testing.T) {
	pf := &Portfolio{Projects: []*Project{
		{Idea: "a", Status: Dropped}, {Idea: "p", Status: Paused}, {Idea: "b", Status: Evaluating, Repo: "o/b"},
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
	r.TicksToday, r.ConsecutiveFailures = 5, 2
	r.Runs = map[string]*Run{"/wt": {Steps: 3, Since: d1.Add(-time.Hour), Last: d1}}
	if err := SaveRuntime(dir, r); err != nil {
		t.Fatal(err)
	}
	r2, _ := LoadRuntime(dir, d1.Add(time.Hour)) // 翌日
	if r2.TicksToday != 0 || r2.ConsecutiveFailures != 2 || r2.Runs != nil {
		t.Fatalf("日付が変わったら tick 数と実行の段の数だけ 0 に: %+v", r2)
	}
	r3, _ := LoadRuntime(dir, d1)
	if r3.TicksToday != 5 || r3.Runs["/wt"] == nil || r3.Runs["/wt"].Steps != 3 || !r3.Runs["/wt"].Since.Equal(d1.Add(-time.Hour)) || !r3.Runs["/wt"].Last.Equal(d1) {
		t.Fatalf("同じ日は持ち越す: %+v", r3)
	}
}

// 実行の見分け: 同じ worktree で、前の段から gap 以内なら続き。gap ちょうどは続き、1 秒でも過ぎれば新しい実行。
// 実行は worktree ごとに別に数える (同時に何本も動く)
func TestRuntimeRun(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 8, 0, 0, 0, time.Local)
	gap := 150 * time.Minute
	r := &Runtime{}
	if r.Current("/a", t0, gap) != nil {
		t.Fatal("何も記録していないのに続き")
	}
	if a := r.Step("/a", t0, gap); a.Steps != 1 || !a.Since.Equal(t0) || !a.Last.Equal(t0) {
		t.Fatalf("1 段目: %+v", a)
	}
	t1 := t0.Add(10 * time.Minute)
	r.Step("/b", t1, gap)
	if a := r.Step("/a", t1, gap); a.Steps != 2 || !a.Since.Equal(t0) || !a.Last.Equal(t1) || r.Runs["/b"].Steps != 1 {
		t.Fatalf("続き (別の worktree とは別に数える): %+v %+v", a, r.Runs["/b"])
	}
	if r.Current("/a", t1.Add(gap), gap) == nil {
		t.Fatal("ちょうど gap は続き")
	}
	if r.Current("/a", t1.Add(gap+time.Second), gap) != nil {
		t.Fatal("gap を過ぎたら新しい実行")
	}
	if r.Current("/c", t1, gap) != nil {
		t.Fatal("別の worktree は新しい実行")
	}
	t2 := t1.Add(gap + time.Second)
	if a := r.Step("/a", t2, gap); a.Steps != 1 || !a.Since.Equal(t2) || !a.Last.Equal(t2) {
		t.Fatalf("使い回しでも、間が空けば 1 段目から: %+v", a)
	}
	if _, ok := r.Runs["/b"]; ok || len(r.Runs) != 1 {
		t.Fatalf("終わった実行の記録が残った: %+v", r.Runs)
	}
}

func TestPortfolioLookup(t *testing.T) {
	pf := &Portfolio{Projects: []*Project{{Idea: "1", Repo: "R/1", Status: Shipped, Started: "2026-11-01", URL: "u"},
		{Idea: "2", Status: Selected, Started: "2026-11-01"}, {Idea: "3", Repo: "R/3", Status: Building, Started: "2026-11-01"}}}
	if a := pf.Actives(); len(a) != 2 || a[0].Idea != "2" || pf.Active().Idea != "2" {
		t.Fatal(a)
	}
	if pf.ByRepo("R/1").Idea != "1" || pf.ByRepo("") != nil || pf.ByRepo("R/x") != nil {
		t.Fatal("ByRepo")
	}
	if pf.ByIdea("3").Repo != "R/3" || pf.ByIdea("1") != nil {
		t.Fatal("ByIdea は作りかけだけ")
	}
	if errs := pf.Validate(1); len(errs) != 1 || !strings.Contains(errs[0], "WIP は 1") {
		t.Fatal(errs)
	}
	if errs := pf.Validate(2); len(errs) != 0 {
		t.Fatal(errs)
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
