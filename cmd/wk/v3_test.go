package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/limits"
	"github.com/Rin0107/ideas/internal/state"
	"github.com/Rin0107/ideas/internal/usage"
)

// v3Factory は状態の clone (state_checkout) を使う工場。戻り値は tick の worktree と clone の場所
func v3Factory(t *testing.T, over map[string]any) (*factory, string, string) {
	t.Helper()
	f := newFactory(t)
	clone := filepath.Join(f.runtime, "factory")
	cfg := map[string]any{"state_checkout": clone}
	for k, v := range over {
		cfg[k] = v
	}
	f.writeConfig(cfg)
	wt := syncRepos(t, f)
	t.Chdir(wt)
	return f, wt, clone
}

func remoteLog(t *testing.T, f *factory) string {
	t.Helper()
	out, err := runGit(f.remote+"/Tester/factory.git", "log", "--format=%s", "main")
	must(t, err)
	return out
}

// transcript は tick の会話の記録 (トークンを数える材料) を置く
func transcript(t *testing.T, f *factory, ts time.Time, tokens int) {
	t.Helper()
	p := filepath.Join(f.home, ".claude", "projects", "-x-auto-wakuwaku-tick-run-1", "s.jsonl")
	b, _ := os.ReadFile(p)
	line := `{"type":"assistant","timestamp":"` + ts.Format(time.RFC3339Nano) + `","message":{"id":"m` + ts.Format("150405.000000000") +
		`","usage":{"input_tokens":` + itoa(tokens) + `,"output_tokens":0}}}` + "\n"
	f.write(p, string(b)+line)
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestV3RecordsGoToClone(t *testing.T) {
	f, wt, clone := v3Factory(t, nil)
	if code := run([]string{"next"}, io.Discard); code != 0 {
		t.Fatal("next", code)
	}
	if _, err := os.Stat(filepath.Join(clone, ".git")); err != nil {
		t.Fatal("clone がない", err)
	}
	transcript(t, f, time.Now(), 42)
	if code := run([]string{"done", "--result", "ok", "--note", "選んだ"}, io.Discard); code != 0 {
		t.Fatal("done", code)
	}
	ticks, err := state.ReadTicks(clone)
	must(t, err)
	if len(ticks) != 1 || ticks[0].Tokens != 42 || ticks[0].Note != "選んだ" {
		t.Fatalf("%+v", ticks)
	}
	if log := remoteLog(t, f); !strings.Contains(log, "wk done --result ok --note 選んだ") {
		t.Fatal("push されていない", log)
	}
	// worktree と手元の main には書かない
	for _, d := range []string{wt, f.root} {
		if _, err := os.Stat(filepath.Join(d, "state", "ticks.jsonl")); err == nil {
			t.Fatal(d, "に書いた")
		}
	}
	// 次の段の wk next が worktree を最新に進める (前の段の記録が読める)
	if code := run([]string{"next"}, io.Discard); code != 0 {
		t.Fatal("next 2", code)
	}
	if _, err := os.Stat(filepath.Join(wt, "state", "ticks.jsonl")); err != nil {
		t.Fatal("worktree が進んでいない", err)
	}
	// worktree に自分の変更があれば進めない
	f.write(filepath.Join(wt, "note.md"), "x")
	refreshWorktree(wt)
	refreshWorktree("")
}

func TestV3SyncIntoClone(t *testing.T) {
	f, wt, clone := v3Factory(t, nil)
	f.write("state/meta.json", `{"dirty":true}`) // 手元の main が汚れていても止まらない
	f.write(filepath.Join(wt, "harness/prompts/x.md"), "手順")
	_, _ = runGit(wt, "add", "-A")
	_, _ = runGit(wt, "commit", "-qm", "手順を直す")
	out, err := capture(t, func() error { stateMode = false; return cmdSync(nil) })
	if err != nil || !strings.Contains(out, "main に merge して push した: work") {
		t.Fatal(out, err)
	}
	if log := remoteLog(t, f); !strings.Contains(log, "手順を直す") {
		t.Fatal(log)
	}
	if _, err := os.Stat(filepath.Join(clone, "harness/prompts/x.md")); err != nil {
		t.Fatal(err)
	}
}

func TestV3SyncErrors(t *testing.T) {
	t.Run("clone を作れない", func(t *testing.T) {
		f, wt, _ := v3Factory(t, nil)
		_, _ = runGit(wt, "commit", "-q", "--allow-empty", "-m", "x")
		f.write(filepath.Join(f.runtime, "factory"), "")
		errHas(t, cmdSync(nil), "clone できない")
	})
	t.Run("worktree の fetch", func(t *testing.T) {
		_, wt, clone := v3Factory(t, nil)
		_, _ = runGit(wt, "commit", "-q", "--allow-empty", "-m", "x")
		must(t, openStateFor(t, clone))
		_, _ = runGit(wt, "remote", "set-url", "origin", "/nonexistent")
		errHas(t, cmdSync(nil), "")
	})
}

// openStateFor は今の worktree の設定で clone を用意する
func openStateFor(t *testing.T, clone string) error {
	t.Helper()
	e, err := load()
	must(t, err)
	return openState(e.cfg)
}

func gitHook(t *testing.T, repo, name, body string) {
	t.Helper()
	p := filepath.Join(repo, ".git", "hooks", name)
	if strings.HasSuffix(repo, ".git") {
		p = filepath.Join(repo, "hooks", name)
	}
	must(t, os.MkdirAll(filepath.Dir(p), 0o755))
	must(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755))
}

func TestV3SettleErrors(t *testing.T) {
	t.Run("origin がない", func(t *testing.T) {
		f, _, _ := v3Factory(t, nil)
		_, _ = runGit(f.root, "remote", "remove", "origin")
		t.Chdir(f.root)
		stateMode = true
		t.Cleanup(func() { stateMode = false })
		_, err := load()
		errHas(t, err, "origin を読めない")
	})
	t.Run("clone の置き場を作れない", func(t *testing.T) {
		f, _, _ := v3Factory(t, nil)
		f.write(filepath.Join(f.runtime, "x"), "")
		f.writeConfig(map[string]any{"state_checkout": filepath.Join(f.runtime, "x", "factory")})
		t.Chdir(f.root)
		e, err := load()
		must(t, err)
		errHas(t, openState(e.cfg), "")
	})
	_, _, clone := v3Factory(t, nil)
	must(t, openStateFor(t, clone))
	t.Run("pull できない", func(t *testing.T) {
		_, _ = runGit(clone, "remote", "set-url", "origin", "/nonexistent")
		errHas(t, settleState(clone, "m"), "最新にできない")
		_, _ = runGit(clone, "remote", "set-url", "origin", "https://github.com/Tester/factory.git")
	})
	t.Run("push できない", func(t *testing.T) {
		f, _, _ := v3Factory(t, nil)
		c2 := filepath.Join(f.runtime, "factory")
		must(t, openStateFor(t, c2))
		gitHook(t, f.remote+"/Tester/factory.git", "pre-receive", "exit 1")
		f.write(filepath.Join(c2, "state", "x"), "1")
		errHas(t, settleState(c2, "m"), "push できない")
		// コマンドの終わりの push の失敗は、終了コードに出る
		stateMode, loaded = true, &env{cfg: mustCfg(t, c2)}
		t.Cleanup(func() { stateMode, loaded = false, nil })
		f.write(filepath.Join(c2, "state", "y"), "1")
		errHas(t, finishState([]string{"done", strings.Repeat("長い", 50)}), "push できない")
	})
	t.Run("commit できない", func(t *testing.T) {
		f, _, _ := v3Factory(t, nil)
		c2 := filepath.Join(f.runtime, "factory")
		must(t, openStateFor(t, c2))
		gitHook(t, c2, "pre-commit", "exit 1")
		f.write(filepath.Join(c2, "state", "x"), "1")
		errHas(t, settleState(c2, "m"), "")
	})
	t.Run("add できない", func(t *testing.T) {
		f, _, _ := v3Factory(t, nil)
		c2 := filepath.Join(f.runtime, "factory")
		must(t, openStateFor(t, c2))
		f.write(filepath.Join(c2, "state", "x"), "1")
		f.write(filepath.Join(c2, ".git", "index.lock"), "")
		errHas(t, settleState(c2, "m"), "")
	})
	t.Run("git でない", func(t *testing.T) {
		errHas(t, settleState(t.TempDir(), "m"), "")
	})
}

func mustCfg(t *testing.T, dir string) *config.Config {
	t.Helper()
	c, err := config.Load(dir)
	must(t, err)
	return c
}

func TestV3RunReportsPushFailure(t *testing.T) {
	f, _, clone := v3Factory(t, nil)
	must(t, openStateFor(t, clone))
	gitHook(t, f.remote+"/Tester/factory.git", "pre-receive", "exit 1")
	f.write(filepath.Join(clone, "state", "w"), "1")
	if code := run([]string{"next"}, io.Discard); code != 1 {
		t.Fatal("push に失敗したのに 0", code)
	}
	// コマンドは通ったが、書いた記録を push できない
	gitHookRemove(t, f)
	if code := run([]string{"next"}, io.Discard); code != 0 {
		t.Fatal("next", code)
	}
	gitHook(t, f.remote+"/Tester/factory.git", "pre-receive", "exit 1")
	var b strings.Builder
	if code := run([]string{"done", "--result", "ok"}, &b); code != 1 || !strings.Contains(b.String(), "記録の push") {
		t.Fatal(code, b.String())
	}
}

func gitHookRemove(t *testing.T, f *factory) {
	t.Helper()
	must(t, os.Remove(filepath.Join(f.remote+"/Tester/factory.git", "hooks", "pre-receive")))
}

func TestV3LoadReloadError(t *testing.T) {
	f, _, clone := v3Factory(t, nil)
	must(t, openStateFor(t, clone))
	// clone の factory.json が壊れている (push はされている前提の、壊れた main)
	f.write(filepath.Join(clone, "factory.json"), "{")
	_, _ = runGit(clone, "commit", "-qam", "壊す")
	_, _ = runGit(clone, "push", "-q", "origin", "HEAD:main")
	stateMode = true
	t.Cleanup(func() { stateMode = false })
	_, err := load()
	errHas(t, err, "factory.json")
}

func TestV3HoursAndBudget(t *testing.T) {
	now := time.Now()
	other := (now.Hour() + 1) % 24
	f, _, _ := v3Factory(t, map[string]any{"run_hours": []int{other}})
	stateMode = true
	t.Cleanup(func() { stateMode = false })
	errHas(t, cmdDue(nil), "動かす時間ではない")
	f.writeConfig(map[string]any{"run_hours": []int{now.Hour()}, "max_tokens_per_day": 100})
	_, _ = runGit(f.root, "commit", "-qam", "cfg")
	_, _ = runGit(f.root, "push", "-q", "origin", "main")
	t.Chdir(f.root)
	stateMode = false
	if _, err := capture(t, func() error { return cmdDue(nil) }); err != nil {
		t.Fatal(err)
	}
	transcript(t, f, now, 150)
	errHas(t, cmdDue(nil), "予算に達した (150/100)")
	out, err := capture(t, func() error { return cmdNext(nil) })
	if err != nil || !strings.Contains(out, "予算に達した") || !strings.Contains(out, `"idle"`) {
		t.Fatal(out, err)
	}
	// 数えられない (記録の場所がディレクトリでない)
	must(t, os.RemoveAll(filepath.Join(f.home, ".claude", "projects")))
	f.write(filepath.Join(f.home, ".claude", "projects"), "")
	errHas(t, cmdDue(nil), "数えられない")
	_, err = capture(t, func() error { return cmdDone([]string{"--result", "ok"}) })
	_ = err
}

func TestV3DoneCountsWithoutUsage(t *testing.T) {
	f := newFactory(t)
	if _, err := capture(t, func() error { return cmdNext(nil) }); err != nil {
		t.Fatal(err)
	}
	f.write(filepath.Join(f.home, ".claude", "projects"), "")
	if _, err := capture(t, func() error { return cmdDone([]string{"--result", "ok"}) }); err != nil {
		t.Fatal(err)
	}
}

func TestV3Usage(t *testing.T) {
	f := newFactory(t)
	now := time.Now()
	transcript(t, f, now, 300)
	must(t, usage.Append(filepath.Join(f.runtime, "usage-claude-p.jsonl"), now, usage.Usage{Tokens: 7}))
	f.projects(active(func(p *state.Project) {
		p.History = []state.Event{{Event: "evaluated", Note: "iterate: 核は動く・わくわく 3・品質 4"}, {Event: "evaluated", Note: "drop: 体験の核が本番で動かない"}}
	}), &state.Project{Idea: "00002-beta", Repo: "Tester/beta", Status: state.Dropped})
	for _, tk := range []state.Tick{{Repo: "Tester/alpha", Tokens: 900, Result: "ok"}, {Repo: "Tester/beta", Tokens: 10, Result: "ok"},
		{Repo: "Tester/gone", Tokens: 5, Result: "ok"}, {Tokens: 1, Result: "ok"}} {
		must(t, state.AppendTick(f.root, tk))
	}
	out, err := capture(t, func() error { return cmdUsage([]string{"--days", "2"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{now.Format(time.DateOnly) + "         307 (トークン 307", "Tester/alpha  900  わくわく 3 → 1 点あたり 300",
		"Tester/beta  10\n", "Tester/gone  5\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q がない:\n%s", want, out)
		}
	}
	errHas(t, cmdUsage([]string{"--x"}), "flag provided but not defined")
	f.write("state/ticks.jsonl", "{")
	errHas(t, cmdUsage(nil), "ticks.jsonl")
	must(t, os.RemoveAll(filepath.Join(f.home, ".claude", "projects")))
	f.write(filepath.Join(f.home, ".claude", "projects"), "")
	errHas(t, cmdUsage(nil), "")
	f.write("factory.json", "{")
	errHas(t, cmdUsage(nil), "factory.json")
}

func TestV3ReviewersAndFactoryMain(t *testing.T) {
	if got := reviewersFor([]string{"src/App.tsx", "spec/README.md"}, []string{"sre"}); strings.Join(got, ",") != "sre,guardian" {
		t.Fatal(got)
	}
	e := &env{cfg: mustCfg(t, newFactory(t).root)}
	if e.factoryMain() != e.cfg.MainCheckout {
		t.Fatal(e.factoryMain())
	}
	e.cfg.StateCheckout = "/x"
	if e.factoryMain() != "/x" {
		t.Fatal(e.factoryMain())
	}
}

func TestV3ScaffoldMVPLimit(t *testing.T) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"max_mvp_issues": 2})
	f.projects(active(func(p *state.Project) { p.Status, p.Repo = state.Selected, "" }))
	errHas(t, cmdScaffold([]string{"--plan", planJSON(t, goodPlan())}), "issue は 1〜2 件 (今 3 件)")
}

func TestV3RefreshFetchFails(t *testing.T) {
	d := t.TempDir()
	_, _ = runGit(d, "init", "-q")
	_, _ = runGit(d, "remote", "add", "origin", "/nonexistent")
	refreshWorktree(d) // 落ちても止まらない
}

func TestV3ClaudeUsage(t *testing.T) {
	newFactory(t)
	e, err := load()
	must(t, err)
	fakeClaude(t, `{"result":"答え","usage":{"input_tokens":5,"output_tokens":6}}`, 0, "0")
	if out, err := runClaude(e, "問い"); err != nil || out != "答え" {
		t.Fatal(out, err)
	}
	u, err := usage.Sum(t.TempDir(), "x", extraUsagePath(e), time.Now().Add(-time.Minute), time.Now().Add(time.Minute))
	if err != nil || u.Tokens != 11 {
		t.Fatal(u, err)
	}
	// 記録できなくても答えは返す
	must(t, os.Remove(extraUsagePath(e)))
	must(t, os.MkdirAll(extraUsagePath(e), 0o755))
	if out, err := runClaude(e, "問い"); err != nil || out != "答え" {
		t.Fatal(out, err)
	}
}

func TestCommitMsg(t *testing.T) {
	got := commitMsg([]string{"iterate", "--plan", "/tmp/claude-1000/-home-someone-x/scratchpad/plan.md", "--note", "a/b を直す"})
	if got != "wk iterate --plan plan.md --note a/b を直す" {
		t.Fatalf("commitMsg = %q", got)
	}
}

// 利用上限 (5 時間・週) の新しい記録があれば、1 日の予算の代わりにそれで止める (docs/DESIGN-v3.md §9)
func TestV3RateLimits(t *testing.T) {
	now := time.Now()
	f := newFactory(t)
	file := filepath.Join(f.runtime, "rate-limits.json")
	f.writeConfig(map[string]any{"max_tokens_per_day": 100, "limits": map[string]any{"file": file, "five_hour_max_pct": 60}})
	transcript(t, f, now, 150)
	put := func(at time.Time, five float64) {
		week := now.Add(-limits.Week / 2).Add(limits.Week).Unix()
		f.write(file, fmt.Sprintf(`{"at":%d,"rate_limits":{"five_hour":{"used_percentage":%v,"resets_at":%d},"seven_day":{"used_percentage":10,"resets_at":%d}}}`,
			at.Unix(), five, now.Add(time.Hour).Unix(), week))
	}
	// 記録がない: 1 日の予算で止める
	errHas(t, cmdDue(nil), "予算に達した (150/100)")
	out, _ := capture(t, func() error { return cmdUsage([]string{"--days", "1"}) })
	if !strings.Contains(out, "利用上限: 新しい記録がない (1 日の予算 100 で止める)") {
		t.Fatal(out)
	}
	// 新しい記録に余裕がある: 1 日の予算を超えていても動く
	put(now, 59)
	if _, err := capture(t, func() error { return cmdDue(nil) }); err != nil {
		t.Fatal(err)
	}
	out, _ = capture(t, func() error { return cmdUsage([]string{"--days", "1"}) })
	if !strings.Contains(out, "利用上限: 5 時間 59% (工場は 60% まで、") {
		t.Fatal(out)
	}
	// 5 時間の上限 (ここでは 60%) に届いた
	put(now, 60)
	errHas(t, cmdDue(nil), "5 時間の利用上限を 60% 使った")
	// 古い記録・壊れた記録は使わず、1 日の予算に戻る
	put(now.Add(-4*time.Hour), 0)
	errHas(t, cmdDue(nil), "予算に達した")
	f.write(file, "{")
	errHas(t, cmdDue(nil), "予算に達した")
}
