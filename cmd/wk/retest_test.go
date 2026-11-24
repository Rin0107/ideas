package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/state"
)

// 計画が固定した受け入れテストを直す (2026-11-24、#107 の AC-3 が CI の速さで落ちた)

func (v *v4fix) retest(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return capture(t, func() error { return cmdRetest(args) })
}

func (v *v4fix) editTask(t *testing.T, n int, mut func(*state.Task)) {
	t.Helper()
	ts, err := state.LoadTasks(v.root, "Tester/alpha")
	must(t, err)
	mut(ts.Get(n))
	must(t, state.SaveTasks(v.root, "Tester/alpha", ts))
}

func TestRetest(t *testing.T) {
	v := v4Factory(t)
	v.runPlan(t)
	before, _ := state.LoadTasks(v.root, "Tester/alpha")
	oldSum := before.Get(11).Tests["tests/one.sh"]
	// 作りかけのコミットがブランチにある
	wt := v.taskWorktree(t, "11")
	v.write(filepath.Join(wt, "src", "one.txt"), "one\n")
	_, _ = runGit(wt, "add", "-A")
	_, _ = runGit(wt, "commit", "-q", "-m", "作りかけ")
	_, _ = runGit(wt, "push", "-q", "origin", "wk/t11")
	t.Chdir(v.root)
	v.write(filepath.Join(v.tests, "tests", "one.sh"), "test -f src/one.txt && test -s src/one.txt\n")
	out, err := v.retest(t, "11", "--dir", v.tests, "--reason", "CI では待ちが足りない")
	if err != nil || !strings.Contains(out, `"branch": "wk/t11"`) {
		t.Fatal(out, err)
	}
	bare := filepath.Join(v.remote, "Tester", "alpha.git")
	log, _ := runGit(bare, "log", "--format=%s", "wk/t11")
	if !strings.HasPrefix(log, "task: 受け入れテストを直す (#11)\n作りかけ\n") {
		t.Fatal("作りかけの上に足していない:", log)
	}
	if body, _ := runGit(bare, "show", "wk/t11:tests/one.sh"); !strings.Contains(body, "test -s") {
		t.Fatal(body)
	}
	if src, err := runGit(bare, "show", "wk/t11:src/one.txt"); err != nil || src != "one\n" {
		t.Fatal("作りかけが消えた", src, err)
	}
	after, _ := state.LoadTasks(v.root, "Tester/alpha")
	got := after.Get(11).Tests["tests/one.sh"]
	if got == oldSum || len(got) != 64 || len(after.Get(11).Tests) != 1 {
		t.Fatal(after.Get(11).Tests)
	}
	for _, call := range []string{
		"issue edit 11 --repo Tester/alpha --remove-label wk:blocked,wk:attempt-1,wk:attempt-2,wk:attempt-3",
		"issue comment 11 --repo Tester/alpha --body wakuwaku factory: 計画が受け入れテストを直した (tests/one.sh)。\n\nCI では待ちが足りない",
	} {
		if !f4called(v, call) {
			t.Fatal(call, v.gh.Calls())
		}
	}
	if _, err := os.Stat(filepath.Join(v.runtime, "plan", "alpha-retest")); err == nil {
		t.Fatal("worktree が残っている")
	}
	// 直したテストなら wk verify は通る (作りかけで満たしている)
	t.Chdir(wt)
	_, _ = runGit(wt, "pull", "-q", "origin", "wk/t11")
	if out, err := capture(t, func() error { return cmdVerify([]string{"11"}) }); err != nil || !strings.Contains(out, `"ok": true`) {
		t.Fatal(out, err)
	}
}

func TestRetestErrors(t *testing.T) {
	changed := "test -f src/one.txt && test -s src/one.txt\n"
	cases := map[string]struct {
		args  []string
		mut   func(t *testing.T, v *v4fix)
		rules []fakegh.Rule
		test  string
		want  string
	}{
		"番号がない":          {args: []string{}, want: "使い方: wk retest"},
		"番号が数でない":        {args: []string{"abc"}, want: "使い方: wk retest"},
		"知らない引数":         {args: []string{"11", "-x"}, want: "-x"},
		"理由がない":          {args: []string{"11", "--reason", " "}, want: "--reason"},
		"設定が壊れている":       {mut: func(_ *testing.T, v *v4fix) { v.write("factory.json", "{") }, want: "factory.json"},
		"v4 でない":         {mut: func(_ *testing.T, v *v4fix) { v.writeConfig(nil) }, want: "v4 が有効でない"},
		"task の tick の中": {mut: func(t *testing.T, v *v4fix) { must(t, writeCurrent(v.env(t), nextActionIssue(11))) }, want: "tick の中では"},
		"作っているものがない":     {mut: func(_ *testing.T, v *v4fix) { v.projects() }, want: "作っているアプリがない"},
		"記録が壊れている":       {mut: func(_ *testing.T, v *v4fix) { v.write("state/tasks/alpha.json", "{") }, want: "alpha.json"},
		"task でない":       {args: []string{"99", "--reason", "r"}, want: "#99 は Tester/alpha の計画の task でない"},
		"テストがない": {mut: func(t *testing.T, v *v4fix) {
			must(t, os.Remove(filepath.Join(v.tests, "tests", "one.sh")))
		}, want: "テスト tests/one.sh"},
		"変わっていない":       {test: "test -f src/one.txt\n", want: "計画のときと同じ"},
		"もう通る":          {test: "true\n", want: "今の main で完了条件が全部通る"},
		"アプリの repo がない": {mut: func(t *testing.T, v *v4fix) { must(t, os.RemoveAll(v.app)) }, want: "fetch"},
		"setup が落ちる": {mut: func(t *testing.T, v *v4fix) {
			v.editTask(t, 11, func(k *state.Task) { k.Setup = "exit 3" })
		}, want: "setup (exit 3) が落ちる"},
		"親がファイル": {mut: func(t *testing.T, v *v4fix) {
			v.editTask(t, 11, func(k *state.Task) { k.Tests = map[string]string{"README.md/x.sh": "0"} })
			v.write(filepath.Join(v.tests, "README.md", "x.sh"), "x\n")
		}, want: "not a directory"},
		"ディレクトリに書く": {mut: func(t *testing.T, v *v4fix) {
			v.editTask(t, 11, func(k *state.Task) { k.Tests = map[string]string{"src": "0"} })
			v.write(filepath.Join(v.tests, "src"), "x\n")
		}, want: "is a directory"},
		"記録を書けない": {mut: func(t *testing.T, v *v4fix) {
			readOnlyDir(t, filepath.Join(v.root, "state", "tasks"))
		}, want: "permission denied"},
		"ラベルを外せない":  {rules: []fakegh.Rule{{Match: `^issue edit 11`, Code: 1}}, want: "issue edit 11"},
		"コメントを書けない": {rules: []fakegh.Rule{{Match: `^issue comment 11`, Code: 1}}, want: "issue comment 11"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			v := v4Factory(t, c.rules...)
			v.runPlan(t)
			test := changed
			if c.test != "" {
				test = c.test
			}
			v.write(filepath.Join(v.tests, "tests", "one.sh"), test)
			if c.mut != nil {
				c.mut(t, v)
			}
			args := c.args
			if args == nil {
				args = []string{"11", "--dir", v.tests, "--reason", "r"}
			}
			_, err := v.retest(t, args...)
			errHas(t, err, c.want)
		})
	}
}

func TestRetestGitFailures(t *testing.T) {
	for _, sub := range []string{"worktree add", "add --", "-m retest", "checkout -q -B wk/retest", "checkout -q --detach", "checkout wk/retest --",
		"受け入れテストを直す", "push -q origin HEAD"} {
		t.Run(sub, func(t *testing.T) {
			v := v4Factory(t)
			v.runPlan(t)
			v.write(filepath.Join(v.tests, "tests", "one.sh"), "test -f src/one.txt && test -s src/one.txt\n")
			failV4Git(t, sub)
			_, err := v.retest(t, "11", "--dir", v.tests, "--reason", "r")
			errHas(t, err, "git が落ちた: "+sub)
		})
	}
}
