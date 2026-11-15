package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/next"
	"github.com/Rin0107/ideas/internal/state"
)

// jsonFile は v を JSON にして一時ファイルに書き、そのパスを返す。
func jsonFile(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	must(t, err)
	p := filepath.Join(t.TempDir(), "in.json")
	must(t, os.WriteFile(p, b, 0o644))
	return p
}

// ghLog は偽物の gh が呼ばれた引数の全部 (呼ばれた順、改行でつなぐ)。
func ghLog(f *factory) string { return strings.Join(f.gh.Calls(), "\n") }

// ---- propose ----

func proposal(title string) PlanIssue {
	return PlanIssue{Title: title, Body: "根拠: CI が 3 回に 1 回落ちる", AC: []string{"同じテストを 20 回流して 20 回とも通る"}}
}

// patrolTick は今の tick を「r の見回り」にする。
func patrolTick(t *testing.T, a next.Action) {
	t.Helper()
	e, err := load()
	must(t, err)
	must(t, writeCurrent(e, a))
}

func patrolAction(r string) next.Action {
	return next.Action{Phase: state.PhasePatrol, Repo: "Tester/alpha", Role: r, Reason: "テスト"}
}

func proposeFactory(t *testing.T, rules ...fakegh.Rule) *factory {
	t.Helper()
	f := newFactory(t, rules...)
	f.writeConfig(map[string]any{"max_proposals": 2})
	f.projects(active())
	patrolTick(t, patrolAction("engineer"))
	return f
}

func TestPropose(t *testing.T) {
	for _, status := range []string{state.Building, state.Iterating} {
		t.Run(status, func(t *testing.T) {
			f := proposeFactory(t)
			f.projects(active(func(p *state.Project) { p.Status = status }))
			mine, theirs := proposal("一つ目"), proposal("二つ目")
			theirs.Role = "designer" // 見つけたのは engineer だが、担当は designer
			out, err := capture(t, func() error {
				return cmdPropose([]string{"--plan", jsonFile(t, proposals{Issues: []PlanIssue{mine, theirs}})})
			})
			if err != nil || out != "engineer の見回り: 提案 2 件 (次の tick で leader が仕分ける)\n" {
				t.Fatal(out, err)
			}
			e, _ := load()
			by := "\n提案: engineer の見回り (" + e.today() + ")\n"
			log := ghLog(f)
			for _, want := range []string{
				"label create wk:proposal --repo Tester/alpha --color c5def5 --force",
				"label create role:designer --repo Tester/alpha --color bfdadc --force",
				"issue create --repo Tester/alpha --title 一つ目 --body " + issueBody(mine) + by + " --label wk:proposal --label role:engineer",
				"issue create --repo Tester/alpha --title 二つ目 --body " + issueBody(theirs) + by + " --label wk:proposal --label role:designer",
			} {
				if !strings.Contains(log, want) {
					t.Fatalf("%q がない:\n%s", want, log)
				}
			}
		})
	}
}

func TestProposeNothing(t *testing.T) {
	f := proposeFactory(t)
	patrolTick(t, patrolAction("sre"))
	out, err := capture(t, func() error { return cmdPropose([]string{"--plan", jsonFile(t, proposals{})}) })
	if err != nil || out != "sre の見回り: 提案なし\n" {
		t.Fatal(out, err)
	}
	if len(f.gh.Calls()) != 0 { // ラベルも issue も作らない
		t.Fatal(f.gh.Calls())
	}
}

func TestProposeErrors(t *testing.T) {
	three := proposals{Issues: []PlanIssue{proposal("a"), proposal("b"), proposal("c")}}
	bad := proposals{Issues: []PlanIssue{{Body: "x", AC: []string{"十分に長い受け入れ条件"}, Role: "leader"}}}
	cases := map[string]struct {
		rules []fakegh.Rule
		setup func(f *factory)
		plan  any
		args  []string
		want  string
	}{
		"知らない引数":      {args: []string{"-x"}, want: "-x"},
		"設定が壊れている":    {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
		"作っているものがない":  {setup: func(f *factory) { f.projects() }, want: "作っているアプリがない"},
		"作っている途中でない":  {setup: func(f *factory) { f.projects(active(func(p *state.Project) { p.Status = state.Evaluating })) }, want: "作っている途中でない (evaluating)"},
		"今の tick がない": {setup: func(f *factory) { must(t, os.Remove(filepath.Join(f.runtime, "current-factory.json"))) }, want: "wk next"},
		"見回りの tick でない": {setup: func(f *factory) {
			patrolTick(t, next.Action{Phase: state.PhaseBuild, Repo: "Tester/alpha", Role: "engineer"})
		}, want: "提案は見回り (patrol) の tick の中だけ (今は build)"},
		"ロールが leader": {setup: func(f *factory) { patrolTick(t, patrolAction("leader")) }, want: "(今は patrol)"},
		"よその repo": {setup: func(f *factory) {
			patrolTick(t, next.Action{Phase: state.PhasePatrol, Repo: "Tester/other", Role: "engineer"})
		}, want: "提案は見回り (patrol) の tick の中だけ"},
		"計画が読めない":     {args: []string{"--plan", "/none.json"}, want: "none.json"},
		"上限を超える":      {plan: three, want: "提案は 1 回の見回りで 2 件まで (今 3 件)"},
		"形が不正":        {plan: bad, want: "提案の形が不正:\n  issues[0]: title が空\n  issues[0]: role \"leader\" は不正"},
		"ラベルを作れない":    {rules: []fakegh.Rule{{Match: `^label create`, Code: 1}}, want: "label create"},
		"issue を作れない": {rules: []fakegh.Rule{{Match: `^issue create`, Code: 1}}, want: "issue create"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := proposeFactory(t, c.rules...)
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				plan := c.plan
				if plan == nil {
					plan = proposals{Issues: []PlanIssue{proposal("a")}}
				}
				args = []string{"--plan", jsonFile(t, plan)}
			}
			_, err := capture(t, func() error { return cmdPropose(args) })
			errHas(t, err, c.want)
		})
	}
}

// ---- triage ----

// triageFactory: 提案が #3 #4 #5、今のマイルストーンに open な issue が 1 件 (#1)。
func triageFactory(t *testing.T, maxOpen int, rules ...fakegh.Rule) *factory {
	t.Helper()
	f := newFactory(t, append(rules,
		fakegh.Rule{Match: `^issue list --repo Tester/alpha --label wk:proposal`, Out: `[{"number":5},{"number":3},{"number":4}]`},
		fakegh.Rule{Match: `^issue list --repo Tester/alpha --milestone MVP`, Out: `[{"number":1,"title":"a","labels":[]}]`})...)
	f.writeConfig(map[string]any{"max_open_issues": maxOpen})
	f.projects(active())
	return f
}

func goodDecisions() []decision {
	return []decision{
		{Issue: 3, Decision: "now", Reason: "核が壊れる"}, // 5 文字 (いちばん短い理由)
		{Issue: 4, Decision: "later", Reason: "次の磨きで直す"},
		{Issue: 5, Decision: "reject", Reason: "  ほかと重なっている  "},
		{Issue: 9, Decision: "now", Reason: "もう提案ではない issue"}, // 提案でないものは見ない (数にも入れない)
	}
}

func TestTriage(t *testing.T) {
	f := triageFactory(t, 2) // 今 1 件 + 今やる 1 件 = 2 件 (ちょうど上限)
	out, err := capture(t, func() error { return cmdTriage([]string{"--decisions", jsonFile(t, goodDecisions())}) })
	if err != nil || out != "仕分けた: 今やる 1・あとで 1・やらない 1\n" {
		t.Fatal(out, err)
	}
	calls := f.gh.Calls()
	want := []string{
		"issue list --repo Tester/alpha --label wk:proposal --state open --limit 100 --json number",
		"issue list --repo Tester/alpha --milestone MVP --state open --limit 100 --json number,title,labels",
		"issue edit 3 --repo Tester/alpha --milestone MVP --remove-label wk:proposal --add-label wk:task",
		"issue comment 3 --repo Tester/alpha --body wakuwaku leader: now — 核が壊れる",
		"issue edit 4 --repo Tester/alpha --remove-label wk:proposal --add-label wk:later",
		"issue comment 4 --repo Tester/alpha --body wakuwaku leader: later — 次の磨きで直す",
		"issue close 5 --repo Tester/alpha --reason not planned",
		"issue comment 5 --repo Tester/alpha --body wakuwaku leader: reject — ほかと重なっている",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("呼び出しが違う:\n%s", strings.Join(calls, "\n"))
	}
}

func TestTriageOverOpenIssues(t *testing.T) {
	ds := goodDecisions()
	ds[1].Decision = "now" // 今 1 件 + 今やる 2 件 = 3 件
	f := triageFactory(t, 2)
	_, err := capture(t, func() error { return cmdTriage([]string{"--decisions", jsonFile(t, ds)}) })
	errHas(t, err, "今やるにすると open な issue が 3 件になる (上限 2)。いくつかを later にする")
	if f.gh.Called("issue edit") || f.gh.Called("issue close") {
		t.Fatal("上限を超えるのに変えた:", f.gh.Calls())
	}
}

func TestTriageShape(t *testing.T) {
	ds := []decision{
		{Issue: 3, Decision: "maybe", Reason: "よく分からない"},
		{Issue: 4, Decision: "later", Reason: "短い理由"}, // 4 文字
		{Issue: 3, Decision: "now", Reason: "やっぱり今やる"},
	}
	f := triageFactory(t, 8)
	_, err := capture(t, func() error { return cmdTriage([]string{"--decisions", jsonFile(t, ds)}) })
	errHas(t, err, "仕分けの形が不正:\n  [0] #3: decision は now / later / reject\n  [1] #4: reason が短すぎる\n  [2] #3: 同じ issue が 2 回ある\n  #5: 結論がない (提案は全部仕分ける)")
	if f.gh.Called("issue edit") || f.gh.Called("issue comment") {
		t.Fatal(f.gh.Calls())
	}
}

func TestTriageErrors(t *testing.T) {
	cases := map[string]struct {
		rules []fakegh.Rule
		setup func(f *factory)
		args  []string
		want  string
	}{
		"知らない引数":      {args: []string{"-x"}, want: "-x"},
		"設定が壊れている":    {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
		"作っている途中でない":  {setup: func(f *factory) { f.projects(active(func(p *state.Project) { p.Status = state.Selected })) }, want: "作っている途中でない (selected)"},
		"仕分けが読めない":    {args: []string{"--decisions", "/none.json"}, want: "none.json"},
		"提案が読めない":     {rules: []fakegh.Rule{{Match: `--label wk:proposal`, Code: 1}}, want: "wk:proposal"},
		"issue が読めない": {rules: []fakegh.Rule{{Match: `--milestone MVP --state`, Code: 1}}, want: "--milestone MVP"},
		"今やるにできない":    {rules: []fakegh.Rule{{Match: `^issue edit 3`, Code: 1}}, want: "issue edit 3"},
		"あとでにできない":    {rules: []fakegh.Rule{{Match: `^issue edit 4`, Code: 1}}, want: "issue edit 4"},
		"閉じられない":      {rules: []fakegh.Rule{{Match: `^issue close 5`, Code: 1}}, want: "issue close 5"},
		"コメントできない":    {rules: []fakegh.Rule{{Match: `^issue comment`, Code: 1}}, want: "issue comment 3"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := triageFactory(t, 8, c.rules...)
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"--decisions", jsonFile(t, goodDecisions())}
			}
			_, err := capture(t, func() error { return cmdTriage(args) })
			errHas(t, err, c.want)
		})
	}
}

// ---- labels ----

func TestLabels(t *testing.T) {
	f := newFactory(t)
	f.projects(active())
	out, err := capture(t, func() error { return cmdLabels([]string{"Tester/alpha"}) })
	if err != nil || !strings.Contains(out, "ラベルをそろえた: Tester/alpha") {
		t.Fatal(out, err)
	}
	for _, l := range []string{"wk:task", "wk:proposal", "wk:later", "role:sre", "role:designer", "role:engineer", "wk:attempt-3"} {
		if !f.gh.Called("label create " + l + " --repo Tester/alpha") {
			t.Fatalf("%s がない: %v", l, f.gh.Calls())
		}
	}
	if f.gh.Called("wk:attempt-4") {
		t.Fatal("試行の上限 (3) を超えたラベルを作った")
	}
}

func TestLabelsErrors(t *testing.T) {
	for name, c := range map[string]struct {
		args  []string
		rules []fakegh.Rule
		setup func(f *factory)
		want  string
	}{
		"引数がない":         {args: nil, want: "使い方"},
		"引数が多い":         {args: []string{"Tester/alpha", "x"}, want: "使い方"},
		"portfolio にない": {args: []string{"Other/x"}, want: "portfolio にない"},
		"ラベルを作れない":      {args: []string{"Tester/alpha"}, rules: []fakegh.Rule{{Match: `^label create`, Code: 1}}, want: "label create"},
		"設定が壊れている":      {args: []string{"Tester/alpha"}, setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t, c.rules...)
			f.projects(active())
			if c.setup != nil {
				c.setup(f)
			}
			_, err := capture(t, func() error { return cmdLabels(c.args) })
			errHas(t, err, c.want)
		})
	}
}
