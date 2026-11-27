package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/state"
)

func planJSON(t *testing.T, p Plan) string {
	t.Helper()
	b, err := json.Marshal(p)
	must(t, err)
	path := filepath.Join(t.TempDir(), "plan.json")
	must(t, os.WriteFile(path, b, 0o644))
	return path
}

func goodPlan() Plan {
	is := PlanIssue{Title: "星を描く", Body: "本文", AC: []string{"明るい星が 10 個以上描かれる"}}
	return Plan{RepoName: "alpha", Description: "星空", Topics: []string{"go"}, Goal: "星空が出る", Issues: []PlanIssue{is, is, is}}
}

// ---- 計画の形 ----

func TestPlanValidate(t *testing.T) {
	if err := goodPlan().validate(true, 8); err != nil {
		t.Fatal(err)
	}
	bad := Plan{RepoName: "Bad_Name", Description: strings.Repeat("あ", 121), Issues: []PlanIssue{{AC: []string{"短い"}}}}
	err := bad.validate(true, 8)
	for _, want := range []string{"repo_name", "description", "topics", "goal", "title が空", "短すぎる"} {
		errHas(t, err, want)
	}
	none := goodPlan()
	none.Issues = nil
	errHas(t, none.validate(true, 8), "issue は 1〜8 件 (今 0 件)")
	errHas(t, goodPlan().validate(true, 2), "issue は 1〜2 件 (今 3 件)")
	tooMany := goodPlan()
	tooMany.Issues[0].AC = []string{"a", "b", "c", "d", "e", "f"}
	errHas(t, tooMany.validate(true, 8), "ac は 1〜5")
	roles := goodPlan()
	roles.Issues[0].Role, roles.Issues[1].Role, roles.Issues[2].Role = "designer", "sre", "engineer"
	if err := roles.validate(true, 8); err != nil {
		t.Fatal("手を動かすロールは通る", err)
	}
	roles.Issues[2].Role = "leader"
	errHas(t, roles.validate(true, 8), "計画の形が不正:\n  issues[2]: role \"leader\" は不正 (engineer / designer / sre)")
	one := Plan{Goal: "g", Issues: goodPlan().Issues[:1]}
	if err := one.validate(false, 5); err != nil {
		t.Fatal("iterate は 1 件から", err)
	}
	if !strings.Contains(issueBody(goodPlan().Issues[0]), "- [ ] AC-1: 明るい星が 10 個以上描かれる") {
		t.Fatal(issueBody(goodPlan().Issues[0]))
	}
}

// ---- select ----

func selectScores(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "s.json")
	must(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

const webAndCLI = `[
 {"idea":"00001-alpha","wakuwaku":5,"light":5,"free":5,"own":5,"findy":5,"data_plan":"なし","stack":"web","reason":"r"},
 {"idea":"00003-gamma","wakuwaku":3,"light":3,"free":3,"own":3,"findy":3,"data_plan":"なし","stack":"go-cli","reason":"r"}]`

func TestSelectSkipsWebWithoutSite(t *testing.T) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"site": map[string]string{"repo": "", "url": ""}})
	if _, err := capture(t, func() error { return cmdSelect([]string{"--scores", selectScores(t, webAndCLI)}) }); err != nil {
		t.Fatal(err)
	}
	if p := f.portfolio().Active(); p.Idea != "00003-gamma" || p.Stack != "go-cli" {
		t.Fatalf("配信の準備前は Web を選ばない: %+v", p)
	}
}

// 主力 (focus) の select: 候補は主力だけ、点数も主力のものだけを使い、stack は web-db (docs/DESIGN-v3.md §10)
func TestSelectFocus(t *testing.T) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"focus": "00003-gamma", "site": map[string]string{"repo": "", "url": ""}})
	f.projects(active()) // 作りかけが上限 (1) でも、主力は始める
	out, err := capture(t, func() error { return cmdCandidates(nil) })
	if err != nil || strings.Count(out, `"Idea"`) != 1 || !strings.Contains(out, `"00003-gamma"`) {
		t.Fatal(out, err)
	}
	_, err = capture(t, func() error { return cmdSelect([]string{"--scores", selectScores(t, webAndCLI)}) })
	errHas(t, err, "主力のアプリの stack は web-db")
	db := `[{"idea":"00001-alpha","wakuwaku":5,"light":5,"free":5,"own":5,"findy":5,"data_plan":"なし","stack":"web","reason":"r"},
 {"idea":"00003-gamma","wakuwaku":2,"light":2,"free":2,"own":2,"findy":2,"data_plan":"D1 に服","stack":"web-db","needs_server":true,"reason":"r"}]`
	if _, err := capture(t, func() error { return cmdSelect([]string{"--scores", selectScores(t, db)}) }); err != nil {
		t.Fatal(err)
	}
	if p := f.portfolio().ByIdea("00003-gamma"); p == nil || p.Stack != "web-db" || p.Status != state.Selected {
		t.Fatalf("%+v", p)
	}
	_, err = capture(t, func() error { return cmdSelect([]string{"--scores", selectScores(t, db)}) })
	errHas(t, err, "主力のアイディア (00003-gamma) はどれも始めている")
}

func TestFocusTemplateReady(t *testing.T) {
	f := newFactory(t)
	e := &env{cfg: mustCfg(t, f.root)}
	f.write("templates/web-db/wk.json", "{}")
	if focusTemplateReady(e) {
		t.Fatal("focus がないのに ready")
	}
	e.cfg.Focus = config.Focus{"00003-gamma"}
	if !focusTemplateReady(e) {
		t.Fatal("雛形があるのに ready でない")
	}
	must(t, os.Remove(filepath.Join(f.root, "templates/web-db/wk.json")))
	if focusTemplateReady(e) {
		t.Fatal("雛形がないのに ready")
	}
}

func TestSelectErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		setup func(f *factory)
		want  string
	}{
		"知らない引数":          {args: []string{"-x"}, want: "-x"},
		"作りかけがある":         {setup: func(f *factory) { f.projects(active()) }, want: "作りかけ"},
		"点数が読めない":         {args: []string{"--scores", "/none.json"}, want: "none.json"},
		"アイディアが読めない":      {setup: func(f *factory) { os.RemoveAll(filepath.Join(f.root, "ideas_wakuwaku")) }, want: "ideas_wakuwaku"},
		"足切りで全部落ちる":       {args: []string{"--scores", ""}, want: "足切り"},
		"idea.md が書けない":   {setup: func(f *factory) { f.block("ideas_wakuwaku/00001-alpha/idea.md") }, want: "idea.md"},
		"portfolio が書けない": {setup: func(f *factory) { f.block("state/portfolio.json.tmp") }, want: "portfolio.json"},
		"設定が壊れている":        {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t)
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"--scores", selectScores(t, webAndCLI)}
			} else if len(args) == 2 && args[1] == "" {
				args = []string{"--scores", selectScores(t, `[{"idea":"00001-alpha","wakuwaku":5,"light":1,"free":5,"own":5,"findy":5,"data_plan":"なし","stack":"web","reason":"r"}]`)}
			}
			_, err := capture(t, func() error { return cmdSelect(args) })
			errHas(t, err, c.want)
		})
	}
}

// ---- scaffold ----

// scaffoldFactory は selected のアプリと、雛形 (templates/web) と、push 先の bare repo を用意する。
func scaffoldFactory(t *testing.T, rules ...fakegh.Rule) *factory {
	f := newFactory(t, append(rules, fakegh.Rule{Match: `^repo view Tester/alpha `, Code: 1})...)
	f.projects(&state.Project{Idea: "00001-alpha", Status: state.Selected, Stack: "web", Started: "2026-11-10", Milestone: "MVP"})
	f.write("templates/web/README.md", "# {{NAME}}\n{{DESCRIPTION}} {{URL}}\n")
	f.write("templates/web/node_modules/x/index.js", "skip")
	must(t, os.MkdirAll(filepath.Join(f.remote, "Tester"), 0o755))
	_, err := runGit(filepath.Join(f.remote, "Tester"), "init", "-q", "--bare", "alpha.git")
	must(t, err)
	return f
}

func TestScaffold(t *testing.T) {
	f := scaffoldFactory(t)
	plan := goodPlan()
	plan.Issues[1].Title, plan.Issues[1].Role = "見た目を決める", "designer"
	if _, err := capture(t, func() error { return cmdScaffold([]string{"--plan", planJSON(t, plan)}) }); err != nil {
		t.Fatal(err)
	}
	log := strings.Join(f.gh.Calls(), "\n")
	for _, want := range []string{
		"issue create --repo Tester/alpha --title 星を描く --body " + issueBody(plan.Issues[0]) + " --milestone MVP --label wk:task --label role:engineer",
		"issue create --repo Tester/alpha --title 見た目を決める --body " + issueBody(plan.Issues[1]) + " --milestone MVP --label wk:task --label role:designer",
		"label create wk:proposal --repo Tester/alpha --color c5def5 --force",
		"label create wk:later --repo Tester/alpha --color d4c5f9 --force",
		"label create role:sre --repo Tester/alpha --color bfdadc --force",
		"label create role:designer --repo Tester/alpha --color bfdadc --force",
		"label create role:engineer --repo Tester/alpha --color bfdadc --force",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("%q がない:\n%s", want, log)
		}
	}
	p := f.portfolio().Active()
	if p.Status != state.Building || p.Repo != "Tester/alpha" {
		t.Fatalf("%+v", p)
	}
	readme, _ := os.ReadFile(filepath.Join(f.apps, "alpha", "README.md"))
	if string(readme) != "# alpha\n星空 (まだ公開していません)\n" {
		t.Fatal(string(readme))
	}
	if _, err := os.Stat(filepath.Join(f.apps, "alpha", "node_modules")); err == nil {
		t.Fatal("node_modules を写した")
	}
	for _, want := range []string{"repo create Tester/alpha --private", "--add-topic go --add-topic wakuwaku-factory",
		"label create wk:attempt-3", "-X POST repos/Tester/alpha/milestones -f title=MVP", "issue create --repo Tester/alpha --title 星を描く"} {
		if !f.gh.Called(want) {
			t.Fatalf("%q が呼ばれていない:\n%s", want, strings.Join(f.gh.Calls(), "\n"))
		}
	}
}

// web-db は templates/web-db から作り、README の URL は Worker の URL (deploy が本番)。サブドメインが空ならまだ決まらない
func TestScaffoldWebDB(t *testing.T) {
	for _, c := range []struct{ sub, want string }{
		{"tester", "# alpha\n星空 https://alpha.tester.workers.dev/\n"},
		{"", "# alpha\n星空 (まだ公開していません)\n"},
	} {
		f := scaffoldFactory(t)
		f.writeConfig(map[string]any{"web_db": map[string]any{"subdomain": c.sub}})
		f.projects(&state.Project{Idea: "00001-alpha", Status: state.Selected, Stack: "web-db", Started: "2026-11-10", Milestone: "MVP"})
		f.write("templates/web-db/README.md", "# {{NAME}}\n{{DESCRIPTION}} {{URL}}\n")
		if _, err := capture(t, func() error { return cmdScaffold([]string{"--plan", planJSON(t, goodPlan())}) }); err != nil {
			t.Fatal(err)
		}
		if readme, _ := os.ReadFile(filepath.Join(f.apps, "alpha", "README.md")); string(readme) != c.want {
			t.Fatal(string(readme))
		}
	}
}

func TestScaffoldResumesWithoutDuplicates(t *testing.T) {
	// 前回 repo の作成まで済んで落ちた: repo はある・マイルストーンも issue もある
	f := newFactory(t,
		fakegh.Rule{Match: `^repo view Tester/alpha `},
		fakegh.Rule{Match: `^api repos/Tester/alpha/milestones --jq`, Out: "MVP\n"},
		fakegh.Rule{Match: `^issue list --repo Tester/alpha`, Out: `[{"number":1,"title":"a","labels":[]}]`},
	)
	f.projects(&state.Project{Idea: "00001-alpha", Repo: "Tester/alpha", Status: state.Selected, Stack: "web", Started: "2026-11-10", Milestone: "MVP"})
	plan := goodPlan()
	plan.Topics = []string{"x"}
	if _, err := capture(t, func() error { return cmdScaffold([]string{"--plan", planJSON(t, plan)}) }); err != nil {
		t.Fatal(err)
	}
	if f.gh.Called("repo create") || f.gh.Called("-X POST") || f.gh.Called("issue create") {
		t.Fatal("二重に作った:", f.gh.Calls())
	}
}

func TestScaffoldErrors(t *testing.T) {
	cases := map[string]struct {
		rules []fakegh.Rule
		plan  func(*Plan)
		setup func(f *factory)
		args  []string
		want  string
	}{
		"知らない引数":          {args: []string{"-x"}, want: "-x"},
		"作っているものがない":      {setup: func(f *factory) { f.projects() }, want: "作っているアプリがない"},
		"selected でない":    {setup: func(f *factory) { f.projects(active()) }, want: "selected でない"},
		"計画が読めない":         {args: []string{"--plan", "/none.json"}, want: "none.json"},
		"計画の形が不正":         {plan: func(p *Plan) { p.Goal = "" }, want: "goal"},
		"よその repo":        {rules: []fakegh.Rule{{Match: `^repo view Tester/alpha `}}, want: "工場のものではない"},
		"portfolio が書けない": {setup: func(f *factory) { f.block("state/portfolio.json.tmp") }, want: "portfolio.json"},
		"作業場所がすでにある":      {setup: func(f *factory) { must(t, os.MkdirAll(filepath.Join(f.apps, "alpha"), 0o755)) }, want: "すでにある"},
		"雛形がない":           {setup: func(f *factory) { os.RemoveAll(filepath.Join(f.root, "templates")) }, want: "templates"},
		"git が使えない":       {setup: func(f *factory) { f.t.Setenv("GIT_DIR", "/nonexistent") }, want: "git"},
		"push できない":       {setup: func(f *factory) { os.RemoveAll(filepath.Join(f.remote, "Tester")) }, want: "push"},
		"topics を付けられない":  {rules: []fakegh.Rule{{Match: `^repo edit`, Code: 1}}, want: "repo edit"},
		"ラベルを作れない":        {rules: []fakegh.Rule{{Match: `^label create`, Code: 1}}, want: "label create"},
		"マイルストーンが読めない":    {rules: []fakegh.Rule{{Match: `milestones --jq`, Code: 1}}, want: "milestones"},
		"マイルストーンを作れない":    {rules: []fakegh.Rule{{Match: `-X POST`, Code: 1}}, want: "POST"},
		"issue が読めない":     {rules: []fakegh.Rule{{Match: `^issue list`, Code: 1}}, want: "issue list"},
		"issue を作れない":     {rules: []fakegh.Rule{{Match: `^issue create`, Code: 1}}, want: "issue create"},
		"履歴が書けない": {setup: func(f *factory) {
			f.block("ideas_wakuwaku/00001-alpha/idea.md")
		}, want: "idea.md"},
		"設定が壊れている": {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := scaffoldFactory(t, c.rules...)
			if c.setup != nil {
				c.setup(f)
			}
			plan := goodPlan()
			if c.plan != nil {
				c.plan(&plan)
			}
			args := c.args
			if args == nil {
				args = []string{"--plan", planJSON(t, plan)}
			}
			_, err := capture(t, func() error { return cmdScaffold(args) })
			errHas(t, err, c.want)
		})
	}
}

func TestCopyTemplateUnreadableFile(t *testing.T) {
	src := t.TempDir()
	p := filepath.Join(src, "secret")
	must(t, os.WriteFile(p, []byte("x"), 0))
	if os.Geteuid() == 0 {
		t.Skip("root は権限で止まらない")
	}
	if copyTemplate(src, filepath.Join(t.TempDir(), "out"), nil) == nil {
		t.Fatal("読めないファイルがあるのに成功")
	}
}

// ---- attempt ----

func TestAttempt(t *testing.T) {
	f := newFactory(t, fakegh.Rule{Match: `^issue list --repo Tester/alpha`, Out: `[{"number":4,"title":"a","labels":[{"name":"wk:attempt-2"}]},{"number":5,"title":"b","labels":[]}]`})
	f.projects(active())
	if _, err := capture(t, func() error { return cmdAttempt([]string{"4", "--note", "型が合わない"}) }); err != nil {
		t.Fatal(err)
	}
	if !f.gh.Called("issue edit 4 --repo Tester/alpha --add-label wk:attempt-3,wk:blocked") || !f.gh.Called("試行 3/3") {
		t.Fatal(f.gh.Calls())
	}
	if _, err := capture(t, func() error { return cmdAttempt([]string{"5"}) }); err != nil || !f.gh.Called("--add-label wk:attempt-1 ") && !f.gh.Called("--add-label wk:attempt-1") {
		t.Fatal(err)
	}
}

func TestAttemptErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		rules []fakegh.Rule
		setup func(f *factory)
		want  string
	}{
		"番号がない":       {args: []string{}, want: "使い方"},
		"番号が数でない":     {args: []string{"x"}, want: "expected integer"},
		"知らない引数":      {args: []string{"4", "-x"}, want: "-x"},
		"作っているものがない":  {setup: func(f *factory) { f.projects() }, want: "作っているアプリがない"},
		"issue が読めない": {rules: []fakegh.Rule{{Match: `^issue list`, Code: 1}}, want: "issue list"},
		"open でない":    {args: []string{"9"}, want: "#9"},
		"ラベルを付けられない":  {rules: []fakegh.Rule{{Match: `^issue edit`, Code: 1}}, want: "issue edit"},
		"コメントできない":    {rules: []fakegh.Rule{{Match: `^issue comment`, Code: 1}}, want: "issue comment"},
		"設定が壊れている":    {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t, append(c.rules, fakegh.Rule{Match: `^issue list`, Out: `[{"number":4,"title":"a","labels":[]}]`})...)
			f.projects(active())
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"4", "--note", "n"}
			}
			_, err := capture(t, func() error { return cmdAttempt(args) })
			errHas(t, err, c.want)
		})
	}
}

// ---- evaluate ----

func evalScores(t *testing.T, core bool, w, q int) string {
	return selectScores(t, `{"core_works":`+map[bool]string{true: "true", false: "false"}[core]+`,"wakuwaku":`+string(rune('0'+w))+
		`,"quality":`+string(rune('0'+q))+`,"bugs":["表示がずれる"],"weakest":"音がない","summary":"動く"}`)
}

func TestEvaluateWritesReport(t *testing.T) {
	f := newFactory(t)
	f.projects(active(func(p *state.Project) { p.History = []state.Event{{Event: "evaluated"}} }))
	out, err := capture(t, func() error { return cmdEvaluate([]string{"--scores", evalScores(t, true, 5, 4)}) })
	if err != nil || !strings.Contains(out, `"verdict": "ship"`) {
		t.Fatal(out, err)
	}
	rep := f.read("reports/evaluations/Tester_alpha-2.md")
	if !strings.Contains(rep, "# 評価 2") || !strings.Contains(rep, "- 表示がずれる") {
		t.Fatal(rep)
	}
	if p := f.portfolio().Active(); p.Status != state.Evaluating || p.Verdict != "ship" {
		t.Fatalf("%+v", p)
	}
}

func TestEvaluateErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		setup func(f *factory)
		want  string
	}{
		"知らない引数":          {args: []string{"-x"}, want: "-x"},
		"作っているものがない":      {setup: func(f *factory) { f.projects() }, want: "作っているアプリがない"},
		"点数が読めない":         {args: []string{"--scores", "/none.json"}, want: "none.json"},
		"点数が範囲の外":         {args: []string{"--scores", ""}, want: "1〜5"},
		"報告の場所が作れない":      {setup: func(f *factory) { f.write("reports", "") }, want: "reports"},
		"報告が書けない":         {setup: func(f *factory) { f.block("reports/evaluations/Tester_alpha-1.md") }, want: "Tester_alpha-1.md"},
		"履歴が書けない":         {setup: func(f *factory) { f.block("ideas_wakuwaku/00001-alpha/idea.md") }, want: "idea.md"},
		"portfolio が書けない": {setup: func(f *factory) { f.block("state/portfolio.json.tmp") }, want: "portfolio.json"},
		"設定が壊れている":        {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t)
			f.projects(active())
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"--scores", evalScores(t, true, 3, 3)}
			} else if len(args) == 2 && args[1] == "" {
				args = []string{"--scores", evalScores(t, true, 9, 3)}
			}
			_, err := capture(t, func() error { return cmdEvaluate(args) })
			errHas(t, err, c.want)
		})
	}
}

// ---- iterate ----

func iterPlan(t *testing.T) string {
	return planJSON(t, Plan{Goal: "音を足す", Issues: []PlanIssue{{Title: "音", AC: []string{"クリックで音が 1 回鳴る"}}}})
}

func TestIterate(t *testing.T) {
	f := newFactory(t)
	f.projects(active(func(p *state.Project) { p.Status, p.Verdict = state.Evaluating, "iterate" }))
	must(t, cmdIterate([]string{"--plan", iterPlan(t)}))
	p := f.portfolio().Active()
	if p.Milestone != "v0.2" || p.Status != state.Iterating || p.Iteration != 1 || p.Verdict != "" {
		t.Fatalf("%+v", p)
	}
	if !f.gh.Called("issue create --repo Tester/alpha --title 音") || !f.gh.Called("--milestone v0.2 --label wk:task --label role:engineer") || !f.gh.Called("title=v0.2") {
		t.Fatal(f.gh.Calls())
	}
}

func TestIterateErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		rules []fakegh.Rule
		setup func(f *factory)
		want  string
	}{
		"知らない引数":          {args: []string{"-x"}, want: "-x"},
		"作っているものがない":      {setup: func(f *factory) { f.projects() }, want: "作っているアプリがない"},
		"結論が iterate でない": {setup: func(f *factory) { f.projects(active()) }, want: "iterate でない"},
		"計画が読めない":         {args: []string{"--plan", "/none.json"}, want: "none.json"},
		"計画の形が不正":         {args: []string{"--plan", ""}, want: "goal"},
		"マイルストーンを作れない":    {rules: []fakegh.Rule{{Match: `-X POST`, Code: 1}}, want: "POST"},
		"issue が読めない":     {rules: []fakegh.Rule{{Match: `^issue list`, Code: 1}}, want: "issue list"},
		"issue を作れない":     {rules: []fakegh.Rule{{Match: `^issue create`, Code: 1}}, want: "issue create"},
		"履歴が書けない":         {setup: func(f *factory) { f.block("ideas_wakuwaku/00001-alpha/idea.md") }, want: "idea.md"},
		"設定が壊れている":        {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t, c.rules...)
			f.projects(active(func(p *state.Project) { p.Status, p.Verdict = state.Evaluating, "iterate" }))
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"--plan", iterPlan(t)}
			} else if len(args) == 2 && args[1] == "" {
				args = []string{"--plan", planJSON(t, Plan{Issues: []PlanIssue{{Title: "x", AC: []string{"十分に長い受け入れ条件"}}}})}
			}
			errHas(t, cmdIterate(args), c.want)
		})
	}
}

// ---- ship (CLI) と公開の点検 ----

func shipRules(url string) []fakegh.Rule {
	return []fakegh.Rule{
		{Match: `^api repos/Tester/alpha/readme`, Out: "# alpha\n" + url + "\n"},
		{Match: `^api repos/Tester/alpha/contents/`, Out: "x"},
		{Match: `^repo view Tester/alpha --json description`, Out: "5 2"},
		{Match: `^run list --repo Tester/alpha`, Out: "success\n"},
	}
}

func TestShipCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	f := newFactory(t, shipRules(srv.URL)...)
	f.projects(active(func(p *state.Project) { p.Stack, p.Status, p.Verdict = "go-cli", state.Evaluating, "ship" }))
	must(t, cmdShip([]string{"--url", srv.URL}))
	pf := f.portfolio()
	if pf.Active() != nil || pf.Projects[0].Status != state.Shipped || pf.Projects[0].URL != srv.URL {
		t.Fatalf("%+v", pf.Projects[0])
	}
	if !f.gh.Called("repo edit Tester/alpha --visibility public") {
		t.Fatal(f.gh.Calls())
	}
	if !f.gh.Called("api -X PATCH repos/Tester/alpha -f security_and_analysis[secret_scanning][status]=enabled -f security_and_analysis[secret_scanning_push_protection][status]=enabled") {
		t.Fatal(f.gh.Calls())
	}
}

func TestPublishCheckFindsProblems(t *testing.T) {
	newFactory(t,
		fakegh.Rule{Match: `^api repos/Tester/alpha/readme`, Out: "no url"},
		fakegh.Rule{Match: `^api repos/Tester/alpha/contents/LICENSE`, Code: 1},
		fakegh.Rule{Match: `^api repos/Tester/alpha/contents/`, Out: "x"},
		fakegh.Rule{Match: `^repo view Tester/alpha --json description`, Out: "0 2"},
		fakegh.Rule{Match: `^run list`, Out: "failure"},
	)
	got := strings.Join(publishCheck("Tester/alpha", "http://127.0.0.1:1/", false), "\n")
	for _, want := range []string{"README に公開の URL", "LICENSE がない", "description か topics", "success でない", "が 0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q がない:\n%s", want, got)
		}
	}
	newFactory(t, fakegh.Rule{Match: `^repo view`, Code: 1})
	got = strings.Join(publishCheck("Tester/alpha", "", false), "\n")
	if !strings.Contains(got, "--url が空") || !strings.Contains(got, "repo view") {
		t.Fatal(got)
	}
}

func TestShipErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	cases := map[string]struct {
		args  []string
		rules []fakegh.Rule
		setup func(f *factory)
		want  string
	}{
		"知らない引数":     {args: []string{"-x"}, want: "-x"},
		"作っているものがない": {setup: func(f *factory) { f.projects() }, want: "作っているアプリがない"},
		"ship でない":   {setup: func(f *factory) { f.projects(active()) }, want: "ship でない"},
		"Web で配信の準備がまだ": {setup: func(f *factory) {
			f.projects(active(func(p *state.Project) { p.Status, p.Verdict = state.Evaluating, "ship" }))
			f.writeConfig(map[string]any{"site": map[string]string{"repo": "x/y", "url": ""}})
		}, want: "配信の準備がまだ"},
		"点検に通らない":      {args: []string{}, want: "--url が空"},
		"public にできない": {rules: []fakegh.Rule{{Match: `^repo edit`, Code: 1}}, want: "repo edit"},
		"鍵の検査を有効にできない": {rules: []fakegh.Rule{{Match: `^api -X PATCH repos/Tester/alpha`, Code: 1}}, want: "PATCH"},
		"個人情報がある": {setup: func(f *factory) {
			privacyScan = func(_ *env, repo string) []string { return []string{"個人情報の検査に通らない: " + repo} }
		}, want: "個人情報の検査に通らない: Tester/alpha"},
		"履歴が書けない":  {setup: func(f *factory) { f.block("ideas_wakuwaku/00001-alpha/idea.md") }, want: "idea.md"},
		"設定が壊れている": {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t, append(c.rules, shipRules(srv.URL)...)...)
			f.projects(active(func(p *state.Project) { p.Stack, p.Status, p.Verdict = "go-cli", state.Evaluating, "ship" }))
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"--url", srv.URL}
			}
			errHas(t, cmdShip(args), c.want)
		})
	}
}

// ---- drop ----

func TestDrop(t *testing.T) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"site": map[string]string{"repo": "", "url": ""}})
	f.projects(active(func(p *state.Project) { p.Status, p.Verdict = state.Evaluating, "drop" }))
	must(t, cmdDrop([]string{"--reason", "核が無料枠で成り立たない", "--never"}))
	if !f.gh.Called("repo archive Tester/alpha --yes") {
		t.Fatal(f.gh.Calls())
	}
	if !strings.Contains(f.read("ideas_wakuwaku/00001-alpha/idea.md"), "(wk:never)") {
		t.Fatal("wk:never が書かれていない")
	}
	// repo を作る前に捨てる (CLI)
	f.projects(&state.Project{Idea: "00003-gamma", Status: state.Selected, Stack: "go-cli", Started: "2026-11-10"})
	must(t, cmdDrop([]string{"--reason", "作る前に重要な情報が要ると分かった"}))
}

func TestDropErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		rules []fakegh.Rule
		setup func(f *factory)
		want  string
	}{
		"知らない引数":       {args: []string{"-x"}, want: "-x"},
		"理由が短い":        {args: []string{"--reason", "だめ"}, want: "10 文字"},
		"作っているものがない":   {setup: func(f *factory) { f.projects() }, want: "作っているアプリがない"},
		"本番から消せない":     {setup: func(f *factory) { f.writeConfig(nil) }, want: "本番から消せない"},
		"archive できない": {rules: []fakegh.Rule{{Match: `^repo archive`, Code: 1}}, want: "archive"},
		"履歴が書けない":      {setup: func(f *factory) { f.block("ideas_wakuwaku/00001-alpha/idea.md") }, want: "idea.md"},
		"設定が壊れている":     {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t, c.rules...)
			f.writeConfig(map[string]any{"site": map[string]string{"repo": "", "url": ""}})
			f.projects(active())
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"--reason", "核が無料枠で成り立たない"}
			}
			errHas(t, cmdDrop(args), c.want)
		})
	}
}

// readOnly は p を読めるが書けないファイルにする (読み込みは通り、書き込みだけ失敗する道を試す)。
func readOnly(t *testing.T, p string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root は権限で止まらない")
	}
	must(t, os.Chmod(p, 0o444))
}

func TestSelectCannotWriteHistory(t *testing.T) {
	f := newFactory(t)
	readOnly(t, filepath.Join(f.root, "ideas_wakuwaku/00003-gamma/idea.md"))
	f.writeConfig(map[string]any{"site": map[string]string{"repo": "", "url": ""}})
	_, err := capture(t, func() error { return cmdSelect([]string{"--scores", selectScores(t, webAndCLI)}) })
	errHas(t, err, "idea.md")
}

func TestScaffoldCannotSaveAtTheEnd(t *testing.T) {
	// issue を作ったあとで portfolio が書けなくなる
	// (偽物の gh は工場の中で動くので、相対パスで壊せる)
	scaffoldFactory(t, fakegh.Rule{Match: `^issue create`, Shell: "mkdir -p state/portfolio.json.tmp/x"})
	_, err := capture(t, func() error { return cmdScaffold([]string{"--plan", planJSON(t, goodPlan())}) })
	errHas(t, err, "portfolio.json")
}

var realPrivacyScan = privacyScan

func TestPrivacyScan(t *testing.T) {
	script, err := filepath.Abs("../../scripts/privacy-scan.mjs")
	must(t, err)
	f := newFactory(t)
	privacyScan = realPrivacyScan
	src, err := os.ReadFile(script)
	must(t, err)
	f.write("scripts/privacy-scan.mjs", string(src))
	e, err := load()
	must(t, err)

	// まだ repo がない: clone できない
	if got := privacyScan(e, "Tester/alpha"); len(got) != 1 || !strings.Contains(got[0], "clone") {
		t.Fatal(got)
	}

	work := filepath.Join(t.TempDir(), "alpha")
	must(t, os.MkdirAll(work, 0o755))
	git := func(args ...string) {
		t.Helper()
		if out, err := runGit(work, args...); err != nil {
			t.Fatal(out, err)
		}
	}
	git("init", "-q")
	must(t, os.WriteFile(filepath.Join(work, "a.txt"), []byte("hello\n"), 0o644))
	git("add", ".")
	git("commit", "-qm", "first")
	must(t, os.MkdirAll(filepath.Join(f.remote, "Tester"), 0o755))
	_, err = runGit(filepath.Join(f.remote, "Tester"), "clone", "-q", "--bare", work, "alpha.git")
	must(t, err)
	if got := privacyScan(e, "Tester/alpha"); got != nil {
		t.Fatal(got)
	}

	// 消したファイルも履歴に残る
	must(t, os.WriteFile(filepath.Join(work, "b.txt"), []byte("someone@gmail.com\n"), 0o644))
	git("add", ".")
	git("commit", "-qm", "second")
	git("rm", "-q", "b.txt")
	git("commit", "-qm", "third")
	git("push", "-q", filepath.Join(f.remote, "Tester", "alpha.git"), "HEAD:main")
	got := privacyScan(e, "Tester/alpha")
	if len(got) != 1 || !strings.Contains(got[0], "個人情報の検査に通らない") || !strings.Contains(got[0], "メールアドレス") || strings.Contains(got[0], "someone") {
		t.Fatal(got)
	}

	// ブランチから消しても、PR の ref に残っていれば止める
	bare := filepath.Join(f.remote, "Tester", "alpha.git")
	must(t, os.RemoveAll(bare))
	_, err = runGit(filepath.Join(f.remote, "Tester"), "init", "-q", "--bare", "alpha.git")
	must(t, err)
	git("push", "-q", bare, "HEAD~2:refs/heads/main", "HEAD~1:refs/pull/1/head")
	got = privacyScan(e, "Tester/alpha")
	if len(got) != 1 || !strings.Contains(got[0], "メールアドレス") {
		t.Fatal(got)
	}

	// 一時ディレクトリを作れない
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "none"))
	if got := privacyScan(e, "Tester/alpha"); len(got) != 1 || !strings.Contains(got[0], "none") {
		t.Fatal(got)
	}
}

// 主力のアプリは ship でも drop でも磨き続ける (docs/DESIGN-v3.md §10)
func TestEvaluateFocusKeepsIterating(t *testing.T) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"focus": []string{"00002-beta", "00001-alpha"}})
	f.projects(active())
	for _, c := range []struct {
		core       bool
		w, q       int
		want, from string
	}{{true, 5, 4, "iterate", "ship"}, {false, 1, 1, "iterate", "drop"}} {
		out, err := capture(t, func() error { return cmdEvaluate([]string{"--scores", evalScores(t, c.core, c.w, c.q)}) })
		if err != nil || !strings.Contains(out, `"verdict": "`+c.want+`"`) || !strings.Contains(out, "主力のアプリなので "+c.from+" にせず磨き続ける") {
			t.Fatal(out, err)
		}
	}
}

// 主力が複数: 候補はまだ始めていない主力だけ、select は書いた順で最初のまだ始めていないもの。全部始めていれば選ばない
func TestSelectFocusList(t *testing.T) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"focus": []string{"00001-alpha", "00003-gamma", "00002-beta"}, "site": map[string]string{"repo": "", "url": ""}})
	f.projects(active()) // 00001-alpha はもう作りかけ
	out, err := capture(t, func() error { return cmdCandidates(nil) })
	if err != nil || strings.Count(out, `"Idea"`) != 2 || strings.Contains(out, `"00001-alpha"`) {
		t.Fatal(out, err)
	}
	db := `[{"idea":"00002-beta","wakuwaku":5,"light":5,"free":5,"own":5,"findy":5,"data_plan":"D1","stack":"web-db","needs_server":true,"reason":"r"},
 {"idea":"00003-gamma","wakuwaku":2,"light":2,"free":2,"own":2,"findy":2,"data_plan":"D1","stack":"web-db","needs_server":true,"reason":"r"}]`
	if _, err := capture(t, func() error { return cmdSelect([]string{"--scores", selectScores(t, db)}) }); err != nil {
		t.Fatal(err)
	}
	if p := f.portfolio().ByIdea("00003-gamma"); p == nil || p.Status != state.Selected || f.portfolio().ByIdea("00002-beta") != nil {
		t.Fatalf("%+v", p)
	}
	e := &env{cfg: mustCfg(t, f.root), pf: f.portfolio()}
	e.pf.Projects = append(e.pf.Projects, &state.Project{Idea: "00002-beta"})
	_, err = chooseIdea(e, nil)
	errHas(t, err, "どれも始めている")
}
