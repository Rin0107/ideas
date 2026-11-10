package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/lock"
	"github.com/Rin0107/ideas/internal/state"
)

func errHas(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), sub) {
		t.Fatalf("エラーに %q がない: %v", sub, err)
	}
}

// ---- load ----

func TestLoadFallsBackToFactory(t *testing.T) {
	f := newFactory(t)
	t.Chdir(t.TempDir()) // 工場の外 (アプリの repo など) から呼ばれた
	t.Setenv("WK_FACTORY", f.root)
	e, err := load()
	if err != nil || e.cfg.Root != f.root {
		t.Fatal(e, err)
	}
	t.Setenv("WK_FACTORY", "")
	if _, err := load(); err == nil { // 既定の ~/src/okuos/repos/ideas は一時の HOME にはない
		t.Fatal("工場がないのに読めた")
	}
	t.Chdir(f.root)
	f.write("state/portfolio.json", "{")
	if _, err := load(); err == nil {
		t.Fatal("壊れた portfolio を読めた")
	}
}

// ---- due ----

func TestDue(t *testing.T) {
	f := newFactory(t)
	out, err := capture(t, func() error { return cmdDue(nil) })
	if err != nil || !strings.Contains(out, "ok (今日 0/6 tick)") {
		t.Fatal(out, err)
	}
	// 生きているロック
	_, err = lock.Acquire(f.runtime, lock.Info{Since: time.Now(), Phase: "build"}, time.Hour)
	must(t, err)
	if code(cmdDue(nil)) != 1 {
		t.Fatal("ロックがあるのに通った")
	}
	must(t, lock.Release(f.runtime))
	// ロックが読めない
	f.block(filepath.Join(f.runtime, "tick.lock"))
	errHas(t, cmdDue(nil), "tick.lock")
	must(t, os.RemoveAll(filepath.Join(f.runtime, "tick.lock")))
	// 今日の上限
	f.write(filepath.Join(f.runtime, "runtime.json"), `{"day":"`+time.Now().Format(time.DateOnly)+`","ticks_today":6}`)
	if err := cmdDue(nil); code(err) != 1 || !strings.Contains(err.Error(), "上限") {
		t.Fatal(err)
	}
	// runtime.json が壊れている
	f.write(filepath.Join(f.runtime, "runtime.json"), "{")
	errHas(t, cmdDue(nil), "runtime.json")
	// STOP
	f.write(filepath.Join(f.runtime, "STOP"), "x")
	if err := cmdDue(nil); code(err) != 1 || !strings.Contains(err.Error(), "STOP") {
		t.Fatal(err)
	}
	// 工場が読めない
	f.write("factory.json", "{")
	if cmdDue(nil) == nil {
		t.Fatal("壊れた設定で通った")
	}
}

// ---- next ----

func TestNextBuildCountsTicks(t *testing.T) {
	f := newFactory(t, fakegh.Rule{Match: `^issue list --repo Tester/alpha`, Out: `[{"number":3,"title":"a","labels":[]}]`})
	f.projects(active())
	out, err := capture(t, func() error { return cmdNext(nil) })
	if err != nil || !strings.Contains(out, `"phase": "build"`) || !strings.Contains(out, `"issue": 3`) {
		t.Fatal(out, err)
	}
	p := f.portfolio().Active()
	if p.Ticks.Total != 1 || p.Ticks.Phase != 1 {
		t.Fatalf("%+v", p.Ticks)
	}
}

func TestNextIdleReleasesLock(t *testing.T) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"min_untried_ideas": 0})
	for _, d := range []string{"00001-alpha", "00002-beta", "00003-gamma"} { // 全部試した
		f.write("ideas_wakuwaku/"+d+"/idea.md", strings.Replace(f.read("ideas_wakuwaku/"+d+"/idea.md"), "# ", "# ", 1)+"\n## 履歴\n- wk:never\n")
	}
	out, err := capture(t, func() error { return cmdNext(nil) })
	if err != nil || !strings.Contains(out, `"phase": "idle"`) {
		t.Fatal(out, err)
	}
	if l, _ := lock.Read(f.runtime); l != nil {
		t.Fatal("idle なのにロックが残った")
	}
}

func TestNextRecordsStaleLockAsAborted(t *testing.T) {
	f := newFactory(t)
	_, err := lock.Acquire(f.runtime, lock.Info{Since: time.Now().Add(-5 * time.Hour), Phase: "build", Worktree: "/old"}, time.Hour)
	must(t, err)
	if _, err := capture(t, func() error { return cmdNext(nil) }); err != nil {
		t.Fatal(err)
	}
	ts, _ := state.ReadTicks(f.root)
	if len(ts) != 1 || ts[0].Result != "aborted" || !strings.Contains(ts[0].Note, "/old") {
		t.Fatal(ts)
	}
	rt, _ := state.LoadRuntime(f.runtime, time.Now())
	if rt.ConsecutiveFailures != 1 || rt.TicksToday != 1 {
		t.Fatalf("%+v", rt)
	}
}

func TestNextErrors(t *testing.T) {
	cases := map[string]func(f *factory){
		"STOP":                func(f *factory) { f.write("STOP", "x") },
		"meta が壊れている":         func(f *factory) { f.write("state/meta.json", "{") },
		"アイディアが読めない":          func(f *factory) { must(t, os.RemoveAll(filepath.Join(f.root, "ideas_wakuwaku"))) },
		"issue が読めない":         func(f *factory) { f.projects(active(func(p *state.Project) { p.Repo = "Tester/broken" })) },
		"ロックが生きている":           func(f *factory) { _, _ = lock.Acquire(f.runtime, lock.Info{Since: time.Now()}, time.Hour) },
		"runtime.json が壊れている": func(f *factory) { f.write(filepath.Join(f.runtime, "runtime.json"), "{") },
		"runtime を書けない":       func(f *factory) { f.block(filepath.Join(f.runtime, "runtime.json.tmp")) },
		"portfolio を書けない": func(f *factory) {
			f.projects(active())
			f.block("state/portfolio.json.tmp")
		},
		"current.json を書けない": func(f *factory) { f.block(filepath.Join(f.runtime, "current.json")) },
		"設定が壊れている":           func(f *factory) { f.write("factory.json", "{") },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t, fakegh.Rule{Match: `^issue list --repo Tester/broken`, Code: 1})
			breakIt(f)
			if _, err := capture(t, func() error { return cmdNext(nil) }); err == nil {
				t.Fatal("失敗するはず")
			}
		})
	}
}

// ---- done ----

func startTick(t *testing.T, f *factory, phase string) {
	t.Helper()
	e, err := load()
	must(t, err)
	_, err = lock.Acquire(f.runtime, lock.Info{Since: time.Now(), Phase: phase}, time.Hour)
	must(t, err)
	must(t, writeCurrent(e, nextAction(phase)))
}

func TestDoneUpdatesMetaForIntakeAndRetro(t *testing.T) {
	f := newFactory(t)
	for _, ph := range []string{state.PhaseIntake, state.PhaseRetro} {
		startTick(t, f, ph)
		if _, err := capture(t, func() error { return cmdDone([]string{"--result", "ok"}) }); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := state.LoadMeta(f.root)
	today := time.Now().Format(time.DateOnly)
	if m.LastIntake != today || m.LastRetro != today {
		t.Fatalf("%+v", m)
	}
}

func TestDoneErrors(t *testing.T) {
	cases := map[string]struct {
		args    []string
		breakIt func(f *factory)
		want    string
	}{
		"知らない引数":              {[]string{"--nope"}, nil, "nope"},
		"result が不正":          {[]string{"--result", "maybe"}, nil, "ok か fail"},
		"設定が壊れている":            {[]string{"--result", "ok"}, func(f *factory) { f.write("factory.json", "{") }, "factory.json"},
		"今の tick がない":         {[]string{"--result", "ok"}, func(f *factory) { os.Remove(filepath.Join(f.runtime, "current.json")) }, "wk next"},
		"current.json が壊れている": {[]string{"--result", "ok"}, func(f *factory) { f.write(filepath.Join(f.runtime, "current.json"), "{") }, "JSON"},
		"runtime.json が壊れている": {[]string{"--result", "ok"}, func(f *factory) { f.write(filepath.Join(f.runtime, "runtime.json"), "{") }, "runtime.json"},
		"meta が壊れている": {[]string{"--result", "ok"}, func(f *factory) {
			setPhase(t, state.PhaseIntake)
			f.write("state/meta.json", "{")
		}, "meta.json"},
		"meta を書けない": {[]string{"--result", "ok"}, func(f *factory) {
			setPhase(t, state.PhaseRetro)
			f.block("state/meta.json.tmp")
		}, "meta.json.tmp"},
		"ticks を書けない":   {[]string{"--result", "fail"}, func(f *factory) { f.block("state/ticks.jsonl") }, "ticks.jsonl"},
		"runtime を書けない": {[]string{"--result", "fail"}, func(f *factory) { f.block(filepath.Join(f.runtime, "runtime.json.tmp")) }, "runtime.json"},
		"ロックを外せない": {[]string{"--result", "ok"}, func(f *factory) {
			must(t, os.Remove(filepath.Join(f.runtime, "tick.lock")))
			f.block(filepath.Join(f.runtime, "tick.lock"))
		}, "tick.lock"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t)
			startTick(t, f, state.PhaseBuild)
			if c.breakIt != nil {
				c.breakIt(f)
			}
			_, err := capture(t, func() error { return cmdDone(c.args) })
			errHas(t, err, c.want)
		})
	}
}

func TestDoneStopsAndReportsIncidentFailure(t *testing.T) {
	f := newFactory(t, fakegh.Rule{Match: `^issue create`, Code: 1})
	f.write(filepath.Join(f.runtime, "runtime.json"), `{"day":"`+time.Now().Format(time.DateOnly)+`","consecutive_failures":2}`)
	startTick(t, f, state.PhaseBuild)
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	_, err := capture(t, func() error { return cmdDone([]string{"--result", "fail", "--note", "x"}) })
	os.Stderr = old
	w.Close()
	b := make([]byte, 4096)
	n, _ := r.Read(b)
	if err != nil || !strings.Contains(string(b[:n]), "incident の作成に失敗") {
		t.Fatal(err, string(b[:n]))
	}
	if _, err := os.Stat(filepath.Join(f.runtime, "STOP")); err != nil {
		t.Fatal("STOP がない")
	}
}

func TestStopWithIncidentCannotWriteStop(t *testing.T) {
	f := newFactory(t)
	e, err := load()
	must(t, err)
	f.block(filepath.Join(f.runtime, "STOP"))
	if stopWithIncident(e, &state.Runtime{}, nextAction("build"), "") == nil {
		t.Fatal("STOP を書けないのに成功")
	}
}

// ---- sync ----

// syncRepos は工場を git にし、origin (bare) と、作業用の worktree を作る。worktree のパスを返す。
func syncRepos(t *testing.T, f *factory) string {
	t.Helper()
	run := func(dir string, args ...string) {
		t.Helper()
		if out, err := runGit(dir, args...); err != nil {
			t.Fatal(out, err)
		}
	}
	run(f.root, "init", "-q", "-b", "main")
	run(f.root, "add", "-A")
	run(f.root, "commit", "-q", "-m", "init")
	run(f.remote, "clone", "-q", "--bare", f.root, "Tester/factory.git")
	run(f.root, "remote", "add", "origin", "https://github.com/Tester/factory.git")
	run(f.root, "fetch", "-q", "origin")
	run(f.root, "branch", "-q", "--set-upstream-to=origin/main", "main")
	wt := filepath.Join(t.TempDir(), "wt")
	run(f.root, "worktree", "add", "-q", "-b", "work", wt)
	return wt
}

func TestSync(t *testing.T) {
	f := newFactory(t)
	wt := syncRepos(t, f)
	t.Chdir(wt)
	must(t, os.WriteFile(filepath.Join(wt, "note.md"), []byte("x"), 0o644))
	errHas(t, cmdSync(nil), "未コミット")
	_, _ = runGit(wt, "add", "-A")
	_, _ = runGit(wt, "commit", "-q", "-m", "note")
	if out, err := capture(t, func() error { return cmdSync(nil) }); err != nil || !strings.Contains(out, "work") {
		t.Fatal(out, err)
	}
	if out, _ := runGit(f.remote+"/Tester/factory.git", "log", "--oneline", "main"); !strings.Contains(out, "note") {
		t.Fatal("push されていない", out)
	}
}

func TestSyncRefuses(t *testing.T) {
	t.Run("main で実行", func(t *testing.T) {
		f := newFactory(t)
		syncRepos(t, f)
		errHas(t, cmdSync(nil), "作業ブランチ")
	})
	t.Run("git でない", func(t *testing.T) {
		newFactory(t)
		if cmdSync(nil) == nil {
			t.Fatal("git でないのに通った")
		}
	})
	t.Run("main が汚れている", func(t *testing.T) {
		f := newFactory(t)
		wt := syncRepos(t, f)
		f.write("ideas_wakuwaku/INDEX.md", "changed")
		t.Chdir(wt)
		errHas(t, cmdSync(nil), "工場の main に未コミット")
	})
	t.Run("pull できない", func(t *testing.T) {
		f := newFactory(t)
		wt := syncRepos(t, f)
		must(t, os.RemoveAll(filepath.Join(f.remote, "Tester")))
		t.Chdir(wt)
		if cmdSync(nil) == nil {
			t.Fatal("pull できないのに通った")
		}
	})
	t.Run("守りの部分が変わっている", func(t *testing.T) {
		f := newFactory(t)
		wt := syncRepos(t, f)
		t.Chdir(wt)
		cfg := strings.Replace(f.read("factory.json"), `"ticks_per_day":6`, `"ticks_per_day":60`, 1)
		must(t, os.WriteFile(filepath.Join(wt, "factory.json"), []byte(cfg), 0o644))
		_, _ = runGit(wt, "commit", "-qam", "evil")
		errHas(t, cmdSync(nil), "守りの部分")
	})
	t.Run("比べられない", func(t *testing.T) {
		f := newFactory(t)
		wt := syncRepos(t, f)
		// 共通の祖先のないブランチ (origin/main...HEAD が計算できない)
		_, _ = runGit(wt, "switch", "-q", "--orphan", "orphan")
		must(t, os.WriteFile(filepath.Join(wt, "factory.json"), []byte(f.read("factory.json")), 0o644))
		_, _ = runGit(wt, "add", "-A")
		_, _ = runGit(wt, "commit", "-q", "-m", "orphan")
		t.Chdir(wt)
		if cmdSync(nil) == nil {
			t.Fatal("比べられないのに通った")
		}
	})
	t.Run("衝突", func(t *testing.T) {
		f := newFactory(t)
		wt := syncRepos(t, f)
		must(t, os.WriteFile(filepath.Join(wt, "a.md"), []byte("worktree"), 0o644))
		_, _ = runGit(wt, "add", "-A")
		_, _ = runGit(wt, "commit", "-q", "-m", "wt")
		f.write("a.md", "main")
		_, _ = runGit(f.root, "add", "-A")
		_, _ = runGit(f.root, "commit", "-q", "-m", "main")
		_, _ = runGit(f.root, "push", "-q", "origin", "main")
		t.Chdir(wt)
		errHas(t, cmdSync(nil), "衝突")
	})
	t.Run("push できない", func(t *testing.T) {
		f := newFactory(t)
		wt := syncRepos(t, f)
		hook := filepath.Join(f.remote, "Tester", "factory.git", "hooks", "pre-receive")
		must(t, os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755))
		must(t, os.WriteFile(filepath.Join(wt, "b.md"), []byte("x"), 0o644))
		_, _ = runGit(wt, "add", "-A")
		_, _ = runGit(wt, "commit", "-q", "-m", "b")
		t.Chdir(wt)
		if cmdSync(nil) == nil {
			t.Fatal("push できないのに通った")
		}
	})
	t.Run("設定が壊れている", func(t *testing.T) {
		f := newFactory(t)
		f.write("factory.json", "{")
		if cmdSync(nil) == nil {
			t.Fatal("通った")
		}
	})
}

// ---- status ----

func TestStatus(t *testing.T) {
	f := newFactory(t)
	out, err := capture(t, func() error { return cmdStatus([]string{"--short"}) })
	if err != nil || !strings.Contains(out, "作っているものはない") {
		t.Fatal(out, err)
	}
	f.projects(active(func(p *state.Project) { p.Preview = "https://p/alpha/"; p.URL = "https://u" }),
		&state.Project{Idea: "00002-beta", Repo: "Tester/beta", Status: state.Shipped, Stack: "web", Started: "2026-11-01", URL: "https://b"})
	out, _ = capture(t, func() error { return cmdStatus([]string{"--short"}) })
	if !strings.Contains(out, "00001-alpha を building") {
		t.Fatal(out)
	}
	out, _ = capture(t, func() error { return cmdStatus(nil) })
	for _, want := range []string{"プレビューの URL: https://p/alpha/", "URL: https://u", "公開時の URL: https://wakuwaku-site.tester.workers.dev/alpha/",
		"shipped 1 / dropped 0", "次の段階: evaluate"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q がない:\n%s", want, out)
		}
	}
	f.writeConfig(map[string]any{"site": map[string]string{"repo": "Tester/wakuwaku-site", "url": ""}})
	f.write("state/meta.json", "{")
	out, _ = capture(t, func() error { return cmdStatus(nil) })
	for _, want := range []string{"まだ決まらない", "配信: 準備がまだ", "判定できない"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q がない:\n%s", want, out)
		}
	}
	f.write(filepath.Join(f.runtime, "STOP"), "止めた\n")
	out, _ = capture(t, func() error { return cmdStatus([]string{"--short"}) })
	if !strings.Contains(out, "停止中 — 止めた") {
		t.Fatal(out)
	}
	f.write("factory.json", "{")
	if cmdStatus(nil) == nil {
		t.Fatal("壊れた設定で通った")
	}
}

// ---- check ----

func TestCheck(t *testing.T) {
	f := newFactory(t)
	out, err := capture(t, func() error { return cmdCheck(nil) })
	if err != nil || !strings.Contains(out, "ok (アイディア 3 件、アプリ 0 件)") {
		t.Fatal(out, err)
	}
	f.projects(&state.Project{Idea: "99999-ghost", Status: state.Building, Started: "2026-11-10"})
	f.write("state/ticks.jsonl", "{\n")
	f.write("state/meta.json", "{")
	must(t, os.Remove(filepath.Join(f.root, "ideas_wakuwaku", "INDEX.md")))
	f.writeConfig(map[string]any{"protected_paths": []string{"factory.json", "missing/"}})
	_, err = capture(t, func() error { return cmdCheck(nil) })
	for _, want := range []string{"99999-ghost", "ticks.jsonl", "meta.json", "INDEX.md を読めない", "missing/ がない"} {
		errHas(t, err, want)
	}
	must(t, os.RemoveAll(filepath.Join(f.root, "ideas_wakuwaku")))
	if cmdCheck(nil) == nil {
		t.Fatal("アイディアがないのに通った")
	}
	f.write("factory.json", "{")
	if cmdCheck(nil) == nil {
		t.Fatal("壊れた設定で通った")
	}
}

// ---- candidates ----

func TestCandidates(t *testing.T) {
	f := newFactory(t)
	out, err := capture(t, func() error { return cmdCandidates([]string{"-n", "2"}) })
	if err != nil || strings.Count(out, `"Idea"`) != 2 {
		t.Fatal(out, err)
	}
	if cmdCandidates([]string{"-x"}) == nil {
		t.Fatal("知らない引数を通した")
	}
	must(t, os.RemoveAll(filepath.Join(f.root, "ideas_wakuwaku")))
	if cmdCandidates(nil) == nil {
		t.Fatal("アイディアがないのに通った")
	}
	f.write("factory.json", "{")
	if cmdCandidates(nil) == nil {
		t.Fatal("壊れた設定で通った")
	}
}

// setPhase は今の tick の段階を書き換える (ロックはそのまま)。
func setPhase(t *testing.T, phase string) {
	t.Helper()
	e, err := load()
	must(t, err)
	must(t, writeCurrent(e, nextAction(phase)))
}
