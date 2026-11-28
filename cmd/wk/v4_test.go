package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/guard"
	"github.com/Rin0107/ideas/internal/next"
	"github.com/Rin0107/ideas/internal/state"
)

// v4 のテストの土台: 工場 (v4 有効)・アプリの repo (手元の bare に push 済み)・計画が書いたテスト

const inboxJSON = `[{"number":5,"title":"朝の思いつき","body":"もっと速く","labels":[]},{"number":6,"title":"直す","body":"","labels":[{"name":"wk:fix"}]}]`

func v4Rules(extra ...fakegh.Rule) []fakegh.Rule {
	return append(extra,
		fakegh.Rule{Match: `^issue list --repo Tester/factory --author Tester`, Out: inboxJSON},
		fakegh.Rule{Match: `^issue list --repo Tester/alpha --author Tester`, Out: `[]`},
		fakegh.Rule{Match: `^api repos/Tester/alpha/milestones --jq`, Out: ""},
		fakegh.Rule{Match: `^issue create --repo Tester/alpha --title one`, Out: "https://github.com/Tester/alpha/issues/11\n"},
		fakegh.Rule{Match: `^issue create --repo Tester/alpha --title two`, Out: "https://github.com/Tester/alpha/issues/12\n"},
		fakegh.Rule{Match: `^issue list --repo Tester/alpha --milestone v0.2`, Out: `[{"number":3,"title":"古い","labels":[]}]`},
		fakegh.Rule{Match: `^issue list --repo Tester/alpha --milestone v0.3`, Out: `[{"number":11,"title":"one","labels":[{"name":"wk:task"}]},{"number":12,"title":"two","labels":[{"name":"wk:task"}]}]`},
	)
}

type v4fix struct {
	*factory
	app, tests, plan string
}

func v4Factory(t *testing.T, extra ...fakegh.Rule) *v4fix {
	t.Helper()
	f := newFactory(t, v4Rules(extra...)...)
	t.Setenv("WK_FACTORY", f.root) // tick はアプリの worktree の中から wk を呼ぶ
	f.writeConfig(map[string]any{"v4": map[string]any{"enabled": true, "min_tasks": 2, "check_timeout_seconds": 5}})
	f.projects(active(func(p *state.Project) {
		p.Status, p.Verdict, p.Milestone, p.Iteration, p.Idea = state.Iterating, "iterate", "v0.2", 1, "00001-alpha"
	}))
	// アプリ: src/ に作る。完了条件は tests/ のスクリプト
	app := filepath.Join(f.apps, "alpha")
	g := func(dir string, args ...string) {
		t.Helper()
		if out, err := runGit(dir, args...); err != nil {
			t.Fatal(out, err)
		}
	}
	must(t, os.MkdirAll(filepath.Join(app, "src"), 0o755))
	must(t, os.WriteFile(filepath.Join(app, "src", "base.txt"), []byte("base\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(app, "README.md"), []byte("alpha\n"), 0o644))
	g(app, "init", "-q", "-b", "main")
	g(app, "add", "-A")
	g(app, "commit", "-q", "-m", "init")
	must(t, os.MkdirAll(filepath.Join(f.remote, "Tester"), 0o755))
	g(filepath.Join(f.remote, "Tester"), "clone", "-q", "--bare", app, "alpha.git")
	g(app, "remote", "add", "origin", "https://github.com/Tester/alpha.git")
	g(app, "fetch", "-q", "origin")
	// 計画の実行が書いたテスト
	tests := t.TempDir()
	f.write(filepath.Join(tests, "tests", "one.sh"), "test -f src/one.txt\n")
	f.write(filepath.Join(tests, "tests", "two.sh"), "grep -q two src/one.txt\n")
	plan := filepath.Join(t.TempDir(), "plan.json")
	f.write(plan, mustJSONString(t, goodV4Plan()))
	return &v4fix{f, app, tests, plan}
}

func mustJSONString(t *testing.T, v any) string {
	b, err := json.Marshal(v)
	must(t, err)
	return string(b)
}

func goodV4Plan() V4Plan {
	return V4Plan{Goal: "速く着替えられる", Setup: "true", Tasks: []V4Task{
		{Title: "one", Purpose: "ファイルを作る", Steps: []string{"src/one.txt を作る"},
			Checks: []state.Check{{Name: "one がある", Run: "sh tests/one.sh"}, {Name: "壊れていない", Run: "test -f src/base.txt"}},
			Tests:  []string{"tests/one.sh"}, Paths: []string{"src/**"}},
		{Title: "two", Purpose: "中身を書く", Steps: []string{"two と書く"}, Role: "designer",
			Checks: []state.Check{{Name: "two", Run: "sh tests/two.sh"}},
			Tests:  []string{"tests/two.sh"}, Paths: []string{"src/one.txt"}, After: []int{0}, From: []string{"Tester/factory#5"}},
	}}
}

func (v *v4fix) runPlan(t *testing.T) string {
	t.Helper()
	out, err := capture(t, func() error { return cmdPlan([]string{"--plan", v.plan, "--dir", v.tests}) })
	if err != nil {
		t.Fatal(out, err)
	}
	return out
}

func TestV4Plan(t *testing.T) {
	v := v4Factory(t)
	out := v.runPlan(t)
	if !strings.Contains(out, `"milestone": "v0.3"`) || !strings.Contains(out, "11") || !strings.Contains(out, "12") {
		t.Fatal(out)
	}
	// テストのブランチが push されている (テストだけが入っている)
	bare := filepath.Join(v.remote, "Tester", "alpha.git")
	for br, file := range map[string]string{"wk/t11": "tests/one.sh", "wk/t12": "tests/two.sh"} {
		files, err := runGit(bare, "show", "--name-only", "--format=", br)
		if err != nil || strings.TrimSpace(files) != file {
			t.Fatal(br, files, err)
		}
	}
	// 完了条件は wk の記録に (issue の本文は写し)
	ts, err := state.LoadTasks(v.root, "Tester/alpha")
	must(t, err)
	t12 := ts.Get(12)
	if ts.Get(11) == nil || t12 == nil || t12.Branch != "wk/t12" || len(t12.After) != 1 || t12.After[0] != 11 ||
		t12.Setup != "true" || len(t12.Tests["tests/two.sh"]) != 64 || t12.Plan == "" || t12.From[0] != "Tester/factory#5" {
		t.Fatalf("%+v", t12)
	}
	p := v.portfolio().ByRepo("Tester/alpha")
	if p.Milestone != "v0.3" || p.Iteration != 2 || p.Verdict != "" || p.Status != state.Iterating || p.Planned != state.PlanNight(time.Now()) {
		t.Fatalf("%+v", p)
	}
	if !strings.Contains(v.read("ideas_wakuwaku/00001-alpha/idea.md"), "計画 v0.3: 速く着替えられる (task 2 件)") {
		t.Fatal(v.read("ideas_wakuwaku/00001-alpha/idea.md"))
	}
	for _, call := range []string{
		"--milestone v0.3 --label wk:task --label role:engineer",
		"--label role:designer",
		"## 先に終わっている task\n- #11",
		"issue close 3 --repo Tester/alpha --comment wakuwaku factory: v0.3 の計画で作り直した",
		"issue close 5 --repo Tester/factory --comment wakuwaku factory: task にした: Tester/alpha#12",
	} {
		if !f4called(v, call) {
			t.Fatal(call, v.gh.Calls())
		}
	}
	if f4called(v, "issue close 6") {
		t.Fatal("工場の issue (wk:fix) は人の issue でない")
	}
	// 計画の worktree は片付いている
	if _, err := os.Stat(filepath.Join(v.runtime, "plan", "alpha")); err == nil {
		t.Fatal("worktree が残っている")
	}
}

// f4called は複数行の本文も含めて探す (fakegh の記録は 1 回の呼び出しが何行かになる)
func f4called(v *v4fix, sub string) bool {
	return strings.Contains(strings.Join(v.gh.Calls(), "\n"), sub)
}

func TestPlanDeclined(t *testing.T) {
	v := v4Factory(t)
	pl := goodV4Plan()
	pl.Tasks[1].From = nil
	pl.Declined = []Declined{{Ref: "Tester/factory#5", Reason: "お金がかかる"}}
	v.write(v.plan, mustJSONString(t, pl))
	v.runPlan(t)
	if !f4called(v, "issue close 5 --repo Tester/factory --comment wakuwaku factory: task にしない: お金がかかる") {
		t.Fatal(v.gh.Calls())
	}
}

func TestPlanFirstVersion(t *testing.T) {
	v := v4Factory(t, fakegh.Rule{Match: `^issue list --repo Tester/alpha --milestone MVP`, Out: `[]`})
	v.projects(active(func(p *state.Project) { p.Idea = "00001-alpha" }))
	v.runPlan(t)
	if p := v.portfolio().ByRepo("Tester/alpha"); p.Milestone != "v0.2" || p.Iteration != 1 {
		t.Fatalf("%+v", p)
	}
}

func TestV4PlanErrors(t *testing.T) {
	cases := map[string]struct {
		mut   func(v *v4fix, pl *V4Plan)
		rules []fakegh.Rule
		want  string
	}{
		"v4 でない":           {mut: func(v *v4fix, _ *V4Plan) { v.writeConfig(nil) }, want: "v4 が有効でない"},
		"評価が iterate でない":  {mut: func(v *v4fix, _ *V4Plan) { v.projects(active(func(p *state.Project) { p.Iteration = 1 })) }, want: "計画できるのは"},
		"計画がない":            {mut: func(v *v4fix, _ *V4Plan) { v.plan = filepath.Join(v.root, "none.json") }, want: "none.json"},
		"形が不正":             {mut: func(_ *v4fix, pl *V4Plan) { pl.Tasks = pl.Tasks[:1] }, want: "task は 2 件以上"},
		"テストがない":           {mut: func(_ *v4fix, pl *V4Plan) { pl.Tasks[0].Tests = []string{"tests/none.sh"} }, want: "tests/none.sh"},
		"もう通る":             {mut: func(_ *v4fix, pl *V4Plan) { pl.Tasks[0].Checks = []state.Check{{Name: "x", Run: "true"}} }, want: "もう全部通る"},
		"setup が落ちる":       {mut: func(_ *v4fix, pl *V4Plan) { pl.Setup = "exit 3" }, want: "setup (exit 3) が落ちる"},
		"人の issue を読めない":   {rules: []fakegh.Rule{{Match: `--author`, Code: 1}}, want: "issue list"},
		"人の issue が壊れている":  {rules: []fakegh.Rule{{Match: `--author`, Out: "{"}}, want: "を読めない"},
		"アプリの repo がない":    {mut: func(v *v4fix, _ *V4Plan) { must(t, os.RemoveAll(v.app)) }, want: "fetch"},
		"マイルストーンを作れない":     {rules: []fakegh.Rule{{Match: `^api repos/Tester/alpha/milestones`, Code: 1}}, want: "milestones"},
		"issue を作れない":      {rules: []fakegh.Rule{{Match: `^issue create`, Code: 1}}, want: "issue create"},
		"issue の番号がない":     {rules: []fakegh.Rule{{Match: `^issue create`, Out: "ok"}}, want: "番号を読めない"},
		"古い issue を読めない":   {rules: []fakegh.Rule{{Match: `^issue list --repo Tester/alpha --milestone v0.2`, Code: 1}}, want: "milestone v0.2"},
		"古い issue を閉じられない": {rules: []fakegh.Rule{{Match: `^issue close 3`, Code: 1}}, want: "issue close 3"},
		"人の issue を閉じられない": {rules: []fakegh.Rule{{Match: `^issue close 5`, Code: 1}}, want: "issue close 5"},
		"知らない引数":           {want: "-x"},
		"設定が壊れている":         {mut: func(v *v4fix, _ *V4Plan) { v.write("factory.json", "{") }, want: "factory.json"},
		"作っているものがない":       {mut: func(v *v4fix, _ *V4Plan) { v.projects() }, want: "作っているアプリがない"},
		"記録を書けない":          {mut: func(v *v4fix, _ *V4Plan) { readOnlyDir(t, filepath.Join(v.root, "state", "tasks")) }, want: "permission denied"},
		"記録が壊れている":         {mut: func(v *v4fix, _ *V4Plan) { v.write("state/tasks/alpha.json", "{") }, want: "alpha.json"},
		"idea.md がない": {mut: func(v *v4fix, _ *V4Plan) {
			must(t, os.Remove(filepath.Join(v.root, "ideas_wakuwaku/00001-alpha/idea.md")))
		}, want: "idea.md"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			v := v4Factory(t, c.rules...)
			pl := goodV4Plan()
			if c.mut != nil {
				c.mut(v, &pl)
			}
			if !strings.Contains(v.plan, "none.json") {
				v.write(v.plan, mustJSONString(t, pl))
			}
			args := []string{"--plan", v.plan, "--dir", v.tests}
			if name == "知らない引数" {
				args = []string{"-x"}
			}
			_, err := capture(t, func() error { return cmdPlan(args) })
			errHas(t, err, c.want)
		})
	}
}

func TestV4PlanValidate(t *testing.T) {
	in := []InboxIssue{{Ref: "Tester/factory#5"}, {Ref: "Tester/factory#7"}}
	gc := guard.Context{Factory: "Tester/factory", Owned: map[string]bool{"Tester/alpha": true}, Protected: appHard}
	pl := V4Plan{Setup: "gh pr merge 1 --repo Tester/alpha", Tasks: []V4Task{
		{Checks: []state.Check{{}}, Tests: []string{"../x", "/abs"}, Paths: []string{"."}, After: []int{0}, Role: "boss", From: []string{"Tester/factory#9"}},
		{Title: "t", Purpose: "p", Steps: []string{"s"}, Checks: []state.Check{{Name: "n", Run: "true"}}, Tests: []string{".claude/settings.json"}, Paths: []string{"src/**"}, After: []int{-1, 0}, From: []string{"Tester/factory#5"}},
		{Title: "t"},
	}, Declined: []Declined{{Ref: "Tester/factory#8"}}}
	err := pl.validate(4, in, gc)
	for _, want := range []string{
		"goal が空", "task は 4 件以上 (今 3 件)", "tasks[0]: title と purpose が要る", "tasks[0]: steps が空", "tasks[0].checks[0]: name と run が要る",
		`tasks[0]: パス "../x"`, `tasks[0]: パス "/abs"`, `tasks[0]: パス "."`, "tasks[0]: after の 0", "tasks[1]: after の -1", `role "boss"`,
		"tasks[0]: from の Tester/factory#9", "tasks[1]: テストを守りの部分に置かない", "tasks[2]: checks が空", "tasks[2]: tests が空", "tasks[2]: paths が空",
		"declined の Tester/factory#8 は人の issue", "declined の Tester/factory#8: reason が空", "Tester/factory#7 (人の issue) を task にするか",
		`コマンド "gh pr merge 1 --repo Tester/alpha"`,
	} {
		errHas(t, err, want)
	}
	if strings.Contains(err.Error(), "tasks[1]: after の 0") || strings.Contains(err.Error(), "Tester/factory#5 (人の issue)") {
		t.Fatal(err)
	}
	good := goodV4Plan()
	if err := good.validate(2, []InboxIssue{{Ref: "Tester/factory#5"}}, gc); err != nil {
		t.Fatal(err)
	}
}

func TestInbox(t *testing.T) {
	v := v4Factory(t)
	out, err := capture(t, func() error { return cmdInbox(nil) })
	if err != nil || !strings.Contains(out, `"ref": "Tester/factory#5"`) || !strings.Contains(out, "もっと速く") || strings.Contains(out, "#6") {
		t.Fatal(out, err)
	}
	// 作りかけのアプリの repo も見る (同じ repo は 1 回)
	if !v.gh.Called("issue list --repo Tester/alpha --author Tester --state open") {
		t.Fatal(v.gh.Calls())
	}
	v.write("factory.json", "{")
	if _, err := capture(t, func() error { return cmdInbox(nil) }); err == nil {
		t.Fatal("壊れた設定で動いた")
	}
}

// taskWorktree は計画のあと、tick が task のブランチで作る場所
func (v *v4fix) taskWorktree(t *testing.T, issue string) string {
	t.Helper()
	wt := filepath.Join(v.apps, ".wt", "alpha-"+issue)
	if out, err := runGit(v.app, "fetch", "-q", "origin"); err != nil {
		t.Fatal(out, err)
	}
	if out, err := runGit(v.app, "worktree", "add", "-q", "-b", "wk/t"+issue, wt, "origin/wk/t"+issue); err != nil {
		t.Fatal(out, err)
	}
	t.Chdir(wt)
	return wt
}

func TestVerify(t *testing.T) {
	v := v4Factory(t)
	v.runPlan(t)
	wt := v.taskWorktree(t, "11")
	// まだ作っていない: 完了条件が通らない
	out, err := capture(t, func() error { return cmdVerify([]string{"11"}) })
	if code(err) != 1 || !strings.Contains(out, `"ok": false`) || !strings.Contains(out, `"name": "one がある"`) {
		t.Fatal(out, err)
	}
	// 作ったがコミットしていない
	v.write(filepath.Join(wt, "src", "one.txt"), "one\n")
	out, err = capture(t, func() error { return cmdVerify([]string{"11"}) })
	if err == nil || !strings.Contains(out, "コミットしていない変更がある") {
		t.Fatal(out, err)
	}
	if _, err := runGit(wt, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(wt, "commit", "-q", "-m", "one"); err != nil {
		t.Fatal(err)
	}
	out, err = capture(t, func() error { return cmdVerify([]string{"11"}) })
	if err != nil || !strings.Contains(out, `"ok": true`) {
		t.Fatal(out, err)
	}
	head, _ := runGit(wt, "rev-parse", "HEAD")
	if loadVerified(v.env(t))["Tester/alpha#11"] != strings.TrimSpace(head) {
		t.Fatal(loadVerified(v.env(t)))
	}
	// テストを変える・範囲の外を変えると通らない (名前を変えたファイルも見る)
	v.write(filepath.Join(wt, "tests", "one.sh"), "true\n")
	v.write(filepath.Join(wt, "README.md"), "x\n")
	_, _ = runGit(wt, "mv", "src/base.txt", "docs.txt")
	out, err = capture(t, func() error { return cmdVerify([]string{"11"}) })
	if err == nil || !strings.Contains(out, "受け入れテスト tests/one.sh が計画のときと違う") || !strings.Contains(out, "変えてよいファイルの外: README.md") ||
		!strings.Contains(out, "変えてよいファイルの外: docs.txt") {
		t.Fatal(out, err)
	}
}

func (v *v4fix) env(t *testing.T) *env {
	t.Helper()
	e, err := load()
	must(t, err)
	return e
}

func TestVerifyFromCurrentTick(t *testing.T) {
	v := v4Factory(t)
	v.runPlan(t)
	wt := v.taskWorktree(t, "11")
	must(t, writeCurrent(v.env(t), nextActionIssue(11)))
	v.write(filepath.Join(wt, "src", "one.txt"), "one\n")
	_, _ = runGit(wt, "add", "-A")
	_, _ = runGit(wt, "commit", "-q", "-m", "one")
	out, err := capture(t, func() error { return cmdVerify(nil) })
	if err != nil || !strings.Contains(out, `"issue": 11`) {
		t.Fatal(out, err)
	}
}

func nextActionIssue(n int) next.Action {
	a := nextAction(state.PhaseTask)
	a.Repo, a.Issue = "Tester/alpha", n
	return a
}

func TestVerifyErrors(t *testing.T) {
	cases := map[string]struct {
		args []string
		mut  func(v *v4fix, wt string)
		want string
	}{
		"番号が数でない":    {args: []string{"x"}, want: "使い方"},
		"task でない":   {args: []string{"99"}, want: "#99 は Tester/alpha の計画の task でない"},
		"git の外":     {mut: func(v *v4fix, _ string) { t.Chdir(t.TempDir()) }, want: "worktree の中"},
		"main がない":   {mut: func(v *v4fix, wt string) { _, _ = runGit(wt, "remote", "remove", "origin") }, want: "merge-base"},
		"setup が落ちる": {mut: func(v *v4fix, _ string) { setSetup(t, v, "exit 4") }, want: "まだ満たしていない"},
		"時間切れ":       {mut: func(v *v4fix, _ string) { setSetup(t, v, "sleep 30") }, want: "まだ満たしていない"},
		"設定が壊れている":   {mut: func(v *v4fix, _ string) { v.write("factory.json", "{") }, want: "factory.json"},
		"記録が壊れている":   {mut: func(v *v4fix, _ string) { v.write("state/tasks/alpha.json", "{") }, want: "alpha.json"},
		"作っているものがない": {mut: func(v *v4fix, _ string) { v.projects() }, want: "作っているアプリがない"},
		"記録の場所に書けない": {mut: func(v *v4fix, wt string) {
			v.write(filepath.Join(wt, "src", "one.txt"), "one\n")
			_, _ = runGit(wt, "add", "-A")
			_, _ = runGit(wt, "commit", "-q", "-m", "one")
			v.block(filepath.Join(v.runtime, "verified.json"))
		}, want: "verified.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			v := v4Factory(t)
			v.runPlan(t)
			wt := v.taskWorktree(t, "11")
			if c.mut != nil {
				c.mut(v, wt)
			}
			args := c.args
			if args == nil {
				args = []string{"11"}
			}
			_, err := capture(t, func() error { return cmdVerify(args) })
			errHas(t, err, c.want)
		})
	}
}

func setSetup(t *testing.T, v *v4fix, s string) {
	ts, err := state.LoadTasks(v.root, "Tester/alpha")
	must(t, err)
	ts.Get(11).Setup = s
	must(t, state.SaveTasks(v.root, "Tester/alpha", ts))
}

func TestTaskMergeProblem(t *testing.T) {
	v := v4Factory(t,
		fakegh.Rule{Match: `^pr view 7 --repo Tester/alpha --json headRefName`, Out: "wk/t11\n"},
		fakegh.Rule{Match: `^pr view 8 --repo Tester/alpha --json headRefName`, Out: "feature\n"},
		fakegh.Rule{Match: `^pr view 9 --repo Tester/alpha --json headRefName`, Out: "wk/t99\n"},
		fakegh.Rule{Match: `^pr view 10`, Code: 1},
	)
	v.runPlan(t)
	e := v.env(t)
	sha := strings.Repeat("a", 40)
	if why, err := taskMergeProblem(e, "Tester/alpha", 7, sha); err != nil || !strings.Contains(why, "wk verify が通っていない (task #11") {
		t.Fatal(why, err)
	}
	must(t, os.WriteFile(verifiedPath(e), []byte(`{"Tester/alpha#11":"`+sha+`"}`), 0o644))
	for pr, want := range map[int]string{7: "", 8: "", 9: ""} {
		if why, err := taskMergeProblem(e, "Tester/alpha", pr, sha); err != nil || why != want {
			t.Fatal(pr, why, err)
		}
	}
	if _, err := taskMergeProblem(e, "Tester/alpha", 10, sha); err == nil {
		t.Fatal("gh が落ちたのに通った")
	}
	// task の tick の中では、その task のブランチの PR だけ
	must(t, writeCurrent(e, nextActionIssue(12)))
	if why, _ := taskMergeProblem(e, "Tester/alpha", 7, sha); !strings.Contains(why, "task #12 の PR は wk/t12 のブランチから出す (今 wk/t11)") {
		t.Fatal(why)
	}
	v.write("state/tasks/alpha.json", "{")
	must(t, writeCurrent(e, nextAction(state.PhaseBuild)))
	if _, err := taskMergeProblem(v.env(t), "Tester/alpha", 7, sha); err == nil {
		t.Fatal("壊れた記録で通った")
	}
	// v3 のままなら見ない
	v.writeConfig(nil)
	if why, err := taskMergeProblem(v.env(t), "Tester/alpha", 10, sha); err != nil || why != "" {
		t.Fatal(why, err)
	}
}

func TestMergeNeedsVerify(t *testing.T) {
	sha := strings.Repeat("b", 40)
	v := v4Factory(t,
		fakegh.Rule{Match: `^pr view 7 --repo Tester/alpha --json author`, Out: "Tester false\n"},
		fakegh.Rule{Match: `^pr view 7 --repo Tester/alpha --json labels`, Out: `["wk:reviewed"]`},
		fakegh.Rule{Match: `^pr view 7 --repo Tester/alpha --json headRefOid`, Out: sha + "\n"},
		fakegh.Rule{Match: `^pr view 7 --repo Tester/alpha --json headRefName`, Out: "wk/t11\n"},
	)
	v.runPlan(t)
	e := v.env(t)
	must(t, recordApproval(e, "Tester/alpha", 7, sha))
	errHas(t, cmdMerge([]string{"Tester/alpha", "7"}), "wk verify が通っていない")
}

func TestNextPlanner(t *testing.T) {
	v := v4Factory(t)
	out, err := capture(t, func() error { return cmdNext([]string{"--planner"}) })
	if err != nil || !strings.Contains(out, `"phase": "plan"`) {
		t.Fatal(out, err)
	}
	errHas(t, cmdNext([]string{"-x"}), "-x")
	_ = v
}

func TestNextTask(t *testing.T) {
	v := v4Factory(t)
	v.runPlan(t)
	out, err := capture(t, func() error { return cmdNext(nil) })
	if err != nil || !strings.Contains(out, `"phase": "task"`) || !strings.Contains(out, `"issue": 11`) {
		t.Fatal(out, err)
	}
	if run([]string{"done", "--result", "ok", "--note", "n"}, io.Discard) != 0 {
		t.Fatal("done")
	}
	v.write("state/tasks/alpha.json", "{")
	if _, err := capture(t, func() error { return cmdNext(nil) }); err == nil {
		t.Fatal("壊れた記録で動いた")
	}
}

func TestAttemptTask(t *testing.T) {
	v := v4Factory(t)
	v.runPlan(t)
	v.gh.Calls()
	if _, err := capture(t, func() error { return cmdAttempt([]string{"11", "--note", "テストが通らない"}) }); err != nil {
		t.Fatal(err)
	}
	if !v.gh.Called("試行 1/2") || !v.gh.Called("--add-label wk:attempt-1") || v.gh.Called("wk:attempt-1,wk:blocked") {
		t.Fatal(v.gh.Calls())
	}
}

// readOnlyDir は dir を作って書き込めなくする (記録を書けない道を通すため)
func readOnlyDir(t *testing.T, dir string) {
	t.Helper()
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// failV4Git は、引数に sub を含む git の呼び出しだけを失敗させる
func failV4Git(t *testing.T, sub string) {
	t.Helper()
	old := git
	git = func(dir string, args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), sub) {
			return "", errors.New("git が落ちた: " + sub)
		}
		return old(dir, args...)
	}
	t.Cleanup(func() { git = old })
}

func TestV4GitFailures(t *testing.T) {
	for _, sub := range []string{"worktree add", "checkout -q -B", "add --", "commit -q", "push -q"} {
		t.Run("plan/"+sub, func(t *testing.T) {
			v := v4Factory(t)
			failV4Git(t, sub)
			_, err := capture(t, func() error { return cmdPlan([]string{"--plan", v.plan, "--dir", v.tests}) })
			errHas(t, err, "git が落ちた: "+sub)
		})
	}
	for _, sub := range []string{"diff --name-only", "status --porcelain", "rev-parse HEAD"} {
		t.Run("verify/"+sub, func(t *testing.T) {
			v := v4Factory(t)
			v.runPlan(t)
			wt := v.taskWorktree(t, "11")
			v.write(filepath.Join(wt, "src", "one.txt"), "one\n")
			_, _ = runGit(wt, "add", "-A")
			_, _ = runGit(wt, "commit", "-q", "-m", "one")
			failV4Git(t, sub)
			_, err := capture(t, func() error { return cmdVerify([]string{"11"}) })
			errHas(t, err, "git が落ちた: "+sub)
		})
	}
}

func TestV4PlanFileFailures(t *testing.T) {
	cases := map[string]struct {
		test string
		mut  func(t *testing.T, v *v4fix)
		want string
	}{
		// テストの置き場所の親がファイル・置き場所がディレクトリ
		"親がファイル":    {test: "README.md/x.sh", want: "not a directory"},
		"ディレクトリに書く": {test: "src", want: "is a directory"},
		"portfolio を書けない": {test: "", mut: func(t *testing.T, v *v4fix) {
			must(t, os.MkdirAll(filepath.Join(v.root, "state", "tasks"), 0o755))
			readOnlyDir(t, filepath.Join(v.root, "state"))
		}, want: "permission denied"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			v := v4Factory(t)
			pl := goodV4Plan()
			if c.test != "" {
				v.write(filepath.Join(v.tests, c.test), "x\n")
				pl.Tasks[0].Tests = []string{c.test}
			}
			v.write(v.plan, mustJSONString(t, pl))
			if c.mut != nil {
				c.mut(t, v)
			}
			_, err := capture(t, func() error { return cmdPlan([]string{"--plan", v.plan, "--dir", v.tests}) })
			errHas(t, err, c.want)
		})
	}
}

func TestVerifyMissingTest(t *testing.T) {
	v := v4Factory(t)
	v.runPlan(t)
	wt := v.taskWorktree(t, "11")
	_, _ = runGit(wt, "rm", "-q", "tests/one.sh")
	_, _ = runGit(wt, "commit", "-q", "-m", "rm")
	out, err := capture(t, func() error { return cmdVerify([]string{"11"}) })
	if err == nil || !strings.Contains(out, "受け入れテスト tests/one.sh が計画のときと違う") {
		t.Fatal(out, err)
	}
}

func TestInboxFails(t *testing.T) {
	newFactory(t, fakegh.Rule{Match: `--author`, Code: 1})
	errHas(t, cmdInbox(nil), "issue list")
}

func TestMergeTaskCheckFails(t *testing.T) {
	sha := strings.Repeat("c", 40)
	v := v4Factory(t,
		fakegh.Rule{Match: `^pr view 7 --repo Tester/alpha --json author`, Out: "Tester false\n"},
		fakegh.Rule{Match: `^pr view 7 --repo Tester/alpha --json labels`, Out: `["wk:reviewed"]`},
		fakegh.Rule{Match: `^pr view 7 --repo Tester/alpha --json headRefOid`, Out: sha + "\n"},
		fakegh.Rule{Match: `^pr view 7 --repo Tester/alpha --json headRefName`, Code: 1},
	)
	must(t, recordApproval(v.env(t), "Tester/alpha", 7, sha))
	errHas(t, cmdMerge([]string{"Tester/alpha", "7"}), "headRefName")
}
