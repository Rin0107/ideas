package main

// v3.1 (docs/DESIGN-v3.md §8): 実行が何本も同時に動き、作りかけを何件も持つ

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/lock"
	"github.com/Rin0107/ideas/internal/next"
	"github.com/Rin0107/ideas/internal/state"
)

func beta(mut ...func(*state.Project)) *state.Project {
	return active(append([]func(*state.Project){func(p *state.Project) { p.Idea, p.Repo = "00002-beta", "Tester/beta" }}, mut...)...)
}

func parallelFactory(t *testing.T) *factory {
	t.Helper()
	f := newFactory(t,
		fakegh.Rule{Match: `^issue list --repo Tester/alpha --milestone`, Out: `[{"number":3,"title":"a","labels":[]}]`},
		fakegh.Rule{Match: `^issue list --repo Tester/beta --milestone`, Out: `[{"number":5,"title":"b","labels":[]}]`})
	f.writeConfig(map[string]any{"max_parallel": 2, "max_wip": 3})
	f.projects(active(), beta())
	return f
}

// 2 本目の実行 (別の worktree) から呼ぶ
func otherRun(t *testing.T, f *factory) string {
	t.Helper()
	t.Setenv("WK_FACTORY", f.root)
	d := filepath.Join(t.TempDir(), "run-2")
	must(t, os.MkdirAll(d, 0o755))
	t.Chdir(d)
	return d
}

func nextOf(t *testing.T) next.Action {
	t.Helper()
	out, err := capture(t, func() error { return cmdNext(nil) })
	must(t, err)
	var a next.Action
	must(t, json.Unmarshal([]byte(out), &a))
	return a
}

func TestParallelRunsTakeDifferentApps(t *testing.T) {
	f := parallelFactory(t)
	if a := nextOf(t); a.Phase != state.PhaseBuild || a.Repo != "Tester/alpha" {
		t.Fatalf("1 本目: %+v", a)
	}
	otherRun(t, f)
	if a := nextOf(t); a.Phase != state.PhaseBuild || a.Repo != "Tester/beta" || a.Issue != 5 {
		t.Fatalf("2 本目は 1 本目のアプリを取らない: %+v", a)
	}
	pf := f.portfolio()
	if pf.ByRepo("Tester/alpha").Ticks.Total != 1 || pf.ByRepo("Tester/beta").Ticks.Total != 1 {
		t.Fatal("それぞれのアプリに数える")
	}
	// 2 本動いていれば 3 本目は始めない
	if err := cmdDue(nil); code(err) != 1 || !strings.Contains(err.Error(), "同時に動ける実行は 2 本まで (今: build ") {
		t.Fatal(err)
	}
	out, err := capture(t, func() error { return cmdStatus(nil) })
	if err != nil || !strings.Contains(out, "動いている実行: 2 本 (同時に 2 本まで、作りかけは 3 件まで)") ||
		!strings.Contains(out, "  factory: build Tester/alpha") || !strings.Contains(out, "  run-2: build Tester/beta") ||
		!strings.Contains(out, "作っているもの: 00002-beta") {
		t.Fatal(out, err)
	}
	out, _ = capture(t, func() error { return cmdStatus([]string{"--short"}) })
	if out != "wakuwaku: 00001-alpha を building (1 tick)・00002-beta を building (1 tick)\n" {
		t.Fatal(out)
	}
	// 選んでいる最中の実行 (全体で 1 つの段・アイディアで持つ) があれば、3 本目は idle
	_, err = lock.Acquire(f.runtime, "run-3", lock.Info{Since: time.Now(), Phase: state.PhaseSelect, Idea: "00003-gamma"}, time.Hour)
	must(t, err)
	t.Chdir(t.TempDir())
	s, err := loadSnapshot(t)
	must(t, err)
	if !s.Claimed[state.PhaseSelect] || !s.Claimed["00003-gamma"] || !s.Claimed["Tester/beta"] || s.Claimed[""] || s.Claimed[state.PhaseBuild] {
		t.Fatalf("%v", s.Claimed)
	}
	if a := next.Decide(s); a.Phase != state.PhaseIdle {
		t.Fatalf("%+v", a)
	}
}

func loadSnapshot(t *testing.T) (next.Snapshot, error) {
	t.Helper()
	e, err := load()
	must(t, err)
	return e.snapshot()
}

func TestParallelReapsStaleRuns(t *testing.T) {
	f := parallelFactory(t)
	_, err := lock.Acquire(f.runtime, "run-9", lock.Info{Since: time.Now().Add(-5 * time.Hour), Phase: "build", Repo: "Tester/alpha",
		Worktree: "/srv/ws/run-9"}, time.Hour)
	must(t, err)
	f.write(filepath.Join(f.runtime, "current-run-9.json"), "{}")
	if a := nextOf(t); a.Repo != "Tester/alpha" {
		t.Fatalf("落ちた実行のアプリは取れる: %+v", a)
	}
	ts, _ := state.ReadTicks(f.root)
	if len(ts) != 1 || ts[0].Result != "aborted" || ts[0].Repo != "Tester/alpha" || ts[0].Note != "ロックが run-9 に残っていた" {
		t.Fatalf("%+v", ts)
	}
	for _, p := range []string{lock.Path(f.runtime, "run-9"), filepath.Join(f.runtime, "current-run-9.json")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatal("残った:", p)
		}
	}
	// ロックが読めないと進まない
	must(t, lock.Release(f.runtime, "factory"))
	must(t, os.MkdirAll(lock.Path(f.runtime, "bad"), 0o755))
	errHas(t, cmdNext(nil), "tick-bad.lock")
	errHas(t, cmdDue(nil), "tick-bad.lock")
	if _, err := loadSnapshot(t); err == nil {
		t.Fatal("ロックが読めないのに判定した")
	}
	e, err := load()
	must(t, err)
	if err := e.reapStale(); err == nil {
		t.Fatal("ロックが読めないのに片付けた")
	}
}

// どのアプリのコマンドか: tick の中なら wk next が渡したもの、アプリの repo の中ならその repo
func TestProjectChoice(t *testing.T) {
	f := parallelFactory(t)
	sel := &state.Project{Idea: "00003-gamma", Status: state.Selected, Started: "2026-11-10", Milestone: "MVP"}
	f.projects(active(), beta(), sel)
	e, err := load()
	must(t, err)
	if _, err := e.project(); err == nil || !strings.Contains(err.Error(), "作りかけが 3 件あり") {
		t.Fatal(err)
	}
	must(t, writeCurrent(e, next.Action{Phase: state.PhaseSelect, Idea: "00003-gamma"}))
	if p, err := e.project(); err != nil || p.Idea != "00003-gamma" {
		t.Fatal(p, err)
	}
	must(t, writeCurrent(e, next.Action{Phase: state.PhaseBuild, Repo: "Tester/beta"}))
	if p, err := e.project(); err != nil || p.Repo != "Tester/beta" {
		t.Fatal(p, err)
	}
	// アプリの repo の中 (tick の外)
	t.Setenv("WK_FACTORY", f.root)
	app := filepath.Join(f.apps, "beta", "src")
	must(t, os.MkdirAll(app, 0o755))
	t.Chdir(app)
	if e, err = load(); err != nil {
		t.Fatal(err)
	}
	if p, err := e.project(); err != nil || p.Repo != "Tester/beta" {
		t.Fatal(p, err)
	}
	t.Chdir(filepath.Join(f.apps, "beta"))
	if p, err := e.project(); err != nil || p.Repo != "Tester/beta" {
		t.Fatal(p, err)
	}
	// task の worktree (AppsDir/.wt/<名前>-t<N>) は元の repo のアプリ
	repo := filepath.Join(f.apps, "beta")
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "x"},
		{"worktree", "add", "-q", filepath.Join(f.apps, ".wt", "beta-t5"), "-b", "wk/t5"}} {
		if _, err := runGit(repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join(f.apps, ".wt", "beta-t5"))
	if p, err := e.project(); err != nil || p.Repo != "Tester/beta" {
		t.Fatal(p, err)
	}
	other := filepath.Join(f.apps, "zeta")
	must(t, os.MkdirAll(other, 0o755))
	t.Chdir(other)
	if _, err := e.project(); err == nil {
		t.Fatal("portfolio にない repo のアプリを返した")
	}
}

// select で決まったアイディアは、この実行のものになる (ほかの実行は選定を再開しない)
func TestSelectClaimsIdea(t *testing.T) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"max_wip": 2})
	f.projects(active())
	scores := selectScores(t, `[{"idea":"00002-beta","wakuwaku":5,"light":5,"free":5,"own":5,"findy":5,"data_plan":"なし","stack":"go-cli","reason":"r"}]`)
	if _, err := capture(t, func() error { return cmdNext(nil) }); err != nil {
		t.Fatal(err)
	}
	if _, err := capture(t, func() error { return cmdSelect([]string{"--scores", scores}) }); err != nil {
		t.Fatal(err)
	}
	l, _ := lock.Read(f.runtime, "factory")
	e, _ := load()
	a, _, _ := readCurrent(e)
	if l.Idea != "00002-beta" || a.Idea != "00002-beta" || l.Phase != state.PhaseEvaluate {
		t.Fatalf("%+v %+v", l, a)
	}
	// 上限まであれば選ばない
	scores = selectScores(t, `[{"idea":"00003-gamma","wakuwaku":5,"light":5,"free":5,"own":5,"findy":5,"data_plan":"なし","stack":"go-cli","reason":"r"}]`)
	errHas(t, cmdSelect([]string{"--scores", scores}), "作りかけが上限まである (2 件。最初は 00001-alpha building)")
	// ロックがない (今の段だけ残っている) と書けない
	f.projects(active())
	must(t, lock.Release(f.runtime, "factory"))
	errHas(t, cmdSelect([]string{"--scores", scores}), "ロックがない")
	if os.Geteuid() != 0 {
		p := e.currentPath("factory")
		must(t, os.Chmod(p, 0o444))
		t.Cleanup(func() { os.Chmod(p, 0o644) })
		errHas(t, bindCurrent(e, "", "x"), "permission denied")
	}
	must(t, os.Remove(e.currentPath("factory")))
	if err := bindCurrent(e, "", "x"); err != nil {
		t.Fatal("tick の外では何もしない:", err)
	}
}

// 状態のロック: clone に同時に 1 つのコマンドしか触らない。読むだけのコマンドは読む間だけ持つ
func TestStateLock(t *testing.T) {
	f, _, clone := v3Factory(t, nil)
	stateWait = 50 * time.Millisecond
	held, err := holdState(&config.Config{RuntimeDir: f.runtime})
	must(t, err)
	if code := run([]string{"next"}, io.Discard); code != 1 {
		t.Fatal("ロックを持たれているのに動いた", code)
	}
	stateMode = false // sync は今の worktree を見るコマンド (run が決める)
	if err := cmdSync(nil); err == nil || !strings.Contains(err.Error(), "状態のロック") {
		t.Fatal(err)
	}
	held()
	// status は読むだけ: 終わったあとロックを持っていない
	stateMode, readOnlyCmd = true, true
	t.Cleanup(func() { stateMode, readOnlyCmd = false, false })
	e, err := load()
	must(t, err)
	if stateMode {
		t.Fatal("読むだけのコマンドが書く形のまま")
	}
	again, err := holdState(e.cfg)
	if err != nil {
		t.Fatal("読むだけのコマンドがロックを持ったまま:", err)
	}
	again()
	// 読むだけのコマンドが書くとき (deploy) は取り直して、最新の状態に当てて push する
	stateMode, readOnlyCmd = true, false
	e, err = load()
	must(t, err)
	e.pf.Projects = append(e.pf.Projects, active())
	must(t, e.save())
	must(t, finishState([]string{"テスト"}))
	releaseState()
	stateMode = false
	p := active()
	must(t, e.update(p, func(q *state.Project) { q.Preview = "https://p/" }))
	if pf, _ := state.LoadPortfolio(clone); pf.ByRepo("Tester/alpha").Preview != "https://p/" || p.Preview != "https://p/" {
		t.Fatal("書けていない")
	}
	if !strings.Contains(remoteLog(t, f), "wk: Tester/alpha の記録") {
		t.Fatal("push されていない")
	}
	errHas(t, e.update(beta(), func(*state.Project) {}), "Tester/beta が portfolio にない")
	held, err = holdState(e.cfg)
	must(t, err)
	errHas(t, e.update(p, func(*state.Project) {}), "状態のロック")
	held()
	f.write(filepath.Join(clone, "state", "portfolio.json.tmp", "x"), "")
	errHas(t, e.update(p, func(*state.Project) {}), "portfolio.json.tmp")
	must(t, os.RemoveAll(filepath.Join(clone, "state", "portfolio.json.tmp")))
	f.write(filepath.Join(clone, "state", "portfolio.json"), "{")
	errHas(t, e.update(p, func(*state.Project) {}), "portfolio.json")
	e.cfg.StateCheckout = filepath.Join(clone, "state", "portfolio.json", "x")
	if err := e.update(p, func(*state.Project) {}); err == nil {
		t.Fatal("clone を開けないのに書いた")
	}
}
