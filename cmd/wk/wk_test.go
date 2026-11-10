package main

// 通しのテスト: 偽物の gh を PATH に置き、tick の流れ (due → next → select → done)、
// 連続失敗での停止 (STOP と incident)、フックの拒否を確かめる。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rin0107/ideas/internal/state"
)

const idea = `# %s

## 概要
a
## わくわくポイント
b
## シーン
c
## 体験の核
d
## ターゲット
e
## マネタイズ
f
## 差別化ポイント
g
`

type fixture struct {
	root, runtime, ghLog string
}

func setup(t *testing.T) fixture {
	t.Helper()
	base := t.TempDir()
	f := fixture{root: filepath.Join(base, "factory"), runtime: filepath.Join(base, "runtime"), ghLog: filepath.Join(base, "gh.log")}
	cfg := map[string]any{
		"owner": "Tester", "main_checkout": f.root, "apps_dir": filepath.Join(base, "apps"), "runtime_dir": f.runtime,
		"ideas_dir": "ideas_wakuwaku", "ticks_per_day": 6, "max_ticks_mvp": 30, "max_ticks_iteration": 15,
		"max_iterations": 2, "max_attempts_per_issue": 3, "max_open_issues": 8, "lock_ttl_minutes": 150,
		"max_consecutive_failures": 3, "intake_every_days": 7, "retro_every_days": 7, "min_untried_ideas": 1,
		"select":          map[string]int{"candidates": 15, "min_free": 3, "min_light": 3},
		"evaluate":        map[string]int{"ship_min_wakuwaku": 4, "ship_min_quality": 3},
		"protected_paths": []string{"factory.json"},
		"site":            map[string]string{"repo": "Tester/wakuwaku-site", "url": "https://wakuwaku-site.tester.workers.dev"},
	}
	b, _ := json.Marshal(cfg)
	must(t, os.MkdirAll(filepath.Join(f.root, "state"), 0o755))
	must(t, os.WriteFile(filepath.Join(f.root, "factory.json"), b, 0o644))
	must(t, os.WriteFile(filepath.Join(f.root, "state", "meta.json"), []byte(`{"last_intake":"2999-01-01"}`), 0o644))
	for i, name := range []string{"00001-alpha", "00002-beta", "00003-gamma"} {
		p := filepath.Join(f.root, "ideas_wakuwaku", name, "idea.md")
		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte(fmt.Sprintf(idea, "アイディア"+fmt.Sprint(i))), 0o644))
	}
	// 偽物の gh: 引数を記録して、空の JSON を返す
	bin := filepath.Join(base, "bin")
	must(t, os.MkdirAll(bin, 0o755))
	must(t, os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\necho \"$@\" >> "+f.ghLog+"\necho '[]'\n"), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(f.root)
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func code(err error) int {
	var e exitError
	if errors.As(err, &e) {
		return e.code
	}
	if err != nil {
		return 1
	}
	return 0
}

func quiet(t *testing.T) {
	t.Helper()
	old := os.Stdout
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	os.Stdout = devnull
	t.Cleanup(func() { os.Stdout = old; devnull.Close() })
}

func TestTickFlow(t *testing.T) {
	f := setup(t)
	quiet(t)

	must(t, cmdDue(nil))
	must(t, cmdNext(nil))
	cur, err := os.ReadFile(filepath.Join(f.runtime, "current.json"))
	must(t, err)
	if !strings.Contains(string(cur), `"phase":"select"`) {
		t.Fatalf("最初は select のはず: %s", cur)
	}
	if code(cmdDue(nil)) != 1 {
		t.Fatal("tick の最中なのに due が通った")
	}

	scores := `[
 {"idea":"00001-alpha","wakuwaku":4,"light":4,"free":4,"own":4,"findy":4,"hardware":false,"sensitive":false,"data_plan":"なし","stack":"web","reason":"r"},
 {"idea":"00002-beta","wakuwaku":5,"light":5,"free":5,"own":5,"findy":5,"hardware":false,"sensitive":true,"data_plan":"メールを持つ","stack":"web","reason":"r"},
 {"idea":"00003-gamma","wakuwaku":3,"light":3,"free":3,"own":3,"findy":3,"hardware":false,"sensitive":false,"data_plan":"なし","stack":"go-cli","reason":"r"}]`
	sp := filepath.Join(t.TempDir(), "scores.json")
	must(t, os.WriteFile(sp, []byte(scores), 0o644))
	must(t, cmdSelect([]string{"--scores", sp}))

	pf, err := state.LoadPortfolio(f.root)
	must(t, err)
	if p := pf.Active(); p == nil || p.Idea != "00001-alpha" || p.Status != state.Selected {
		t.Fatalf("sensitive の beta を除いて alpha が選ばれるはず: %+v", p)
	}
	md, _ := os.ReadFile(filepath.Join(f.root, "ideas_wakuwaku", "00001-alpha", "idea.md"))
	if !strings.Contains(string(md), "## 履歴") {
		t.Fatal("idea.md に履歴が足されていない")
	}
	if code(cmdSelect([]string{"--scores", sp})) == 0 {
		t.Fatal("作りかけがあるのに 2 つ目を選べた")
	}

	must(t, cmdDone([]string{"--result", "ok", "--note", "選定"}))
	if _, err := os.Stat(filepath.Join(f.runtime, "tick.lock")); err == nil {
		t.Fatal("done のあとにロックが残っている")
	}
	ticks, _ := state.ReadTicks(f.root)
	if len(ticks) != 1 || ticks[0].Phase != "select" || ticks[0].Result != "ok" {
		t.Fatalf("ticks: %+v", ticks)
	}
	if code(cmdDone([]string{"--result", "ok"})) == 0 {
		t.Fatal("tick がないのに done が通った")
	}
}

func TestStopsAfterConsecutiveFailures(t *testing.T) {
	f := setup(t)
	quiet(t)
	for i := 0; i < 3; i++ {
		must(t, cmdDue(nil))
		must(t, cmdNext(nil))
		must(t, cmdDone([]string{"--result", "fail", "--note", "テスト"}))
	}
	if _, err := os.Stat(filepath.Join(f.runtime, "STOP")); err != nil {
		t.Fatal("3 回失敗しても STOP ができていない")
	}
	log, _ := os.ReadFile(f.ghLog)
	if !strings.Contains(string(log), "issue create --repo Tester/factory") || !strings.Contains(string(log), "wk:incident") {
		t.Fatalf("incident が立っていない: %s", log)
	}
	if code(cmdDue(nil)) != 1 {
		t.Fatal("STOP があるのに due が通った")
	}
	if cmdNext(nil) == nil {
		t.Fatal("STOP があるのに next が通った")
	}
}

func TestDailyBudget(t *testing.T) {
	f := setup(t)
	quiet(t)
	must(t, os.MkdirAll(f.runtime, 0o755))
	rt, _ := state.LoadRuntime(f.runtime, nowForTest())
	rt.TicksToday = 6
	must(t, state.SaveRuntime(f.runtime, rt))
	if code(cmdDue(nil)) != 1 {
		t.Fatal("今日の上限なのに due が通った")
	}
}

func TestGuardHooks(t *testing.T) {
	f := setup(t)
	quiet(t)
	hook := func(fn func([]string) error, input map[string]any) int {
		input["cwd"] = f.root
		b, _ := json.Marshal(input)
		r, w, _ := os.Pipe()
		_, _ = w.Write(b)
		w.Close()
		old := os.Stdin
		os.Stdin = r
		defer func() { os.Stdin = old }()
		return code(fn(nil))
	}
	bash := func(cmd string) int {
		return hook(cmdGuardBash, map[string]any{"tool_name": "Bash", "tool_input": map[string]string{"command": cmd}})
	}
	edit := func(path string) int {
		return hook(cmdGuardEdit, map[string]any{"tool_name": "Edit", "tool_input": map[string]string{"file_path": path}})
	}
	if bash("gh repo delete Tester/x --yes") != 2 || bash("cat ~/.config/wakuwaku/env") != 2 {
		t.Fatal("危ないコマンドを通した")
	}
	if bash("go test ./...") != 0 {
		t.Fatal("普通のコマンドを止めた")
	}
	if edit(filepath.Join(f.root, "factory.json")) != 2 {
		t.Fatal("守りの部分の編集を通した")
	}
	if edit(filepath.Join(f.root, "ideas_wakuwaku", "00001-alpha", "idea.md")) != 0 {
		t.Fatal("アイディアの編集を止めた")
	}
}

func nowForTest() time.Time { return time.Now() }
