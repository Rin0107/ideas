package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/role"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

// fakeClaude は PATH に偽物の claude を置く。out を標準出力に書き、code で終わる (sleep 秒だけ待ってから)。
func fakeClaude(t *testing.T, out string, code int, sleep string) {
	t.Helper()
	d := t.TempDir()
	must(t, os.WriteFile(filepath.Join(d, "out"), []byte(out), 0o644))
	sh := "#!/bin/sh\ncat > /dev/null\nsleep " + sleep + "\ncat " + filepath.Join(d, "out") + "\nexit " + fmt.Sprint(code) + "\n"
	must(t, os.WriteFile(filepath.Join(d, "claude"), []byte(sh), 0o755))
	t.Setenv("PATH", d+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// reviewRules は PR #5 (Tester/alpha) を見るための gh の決まり。
func reviewRules(extra ...fakegh.Rule) []fakegh.Rule {
	return append(extra,
		fakegh.Rule{Match: `^pr view 5 --repo Tester/alpha --json headRefOid`, Out: sha},
		fakegh.Rule{Match: `^pr diff 5`, Out: "+a\n"},
		fakegh.Rule{Match: `^pr view 5 --repo Tester/alpha --json title`, Out: `{"title":"t","body":"Closes #1","closingIssuesReferences":[{"number":1}]}`},
		fakegh.Rule{Match: `^issue view 1`, Out: "#1 t\nAC-1-1"},
		fakegh.Rule{Match: `^pr checks 5`, Out: `[{"name":"verify","bucket":"pass"}]`},
	)
}

func reviewFactory(t *testing.T, extra ...fakegh.Rule) *factory {
	f := newFactory(t, reviewRules(extra...)...)
	f.projects(active())
	for _, r := range append(role.Reviewers([]string{"src/app.tsx"}), guardianReviewer) {
		f.write("harness/reviewers/"+r+".md", "# reviewer:"+r+"\n")
	}
	return f
}

func TestReviewApprove(t *testing.T) {
	f := reviewFactory(t)
	fakeClaude(t, "```json\n{\"verdict\":\"approve\",\"issues\":[\"小さな点\"],\"summary\":\"よい\"}\n```", 0, "0")
	if _, err := capture(t, func() error { return cmdReview([]string{"Tester/alpha", "5"}) }); err != nil {
		t.Fatal(err)
	}
	if !f.gh.Called("pr edit 5 --repo Tester/alpha --add-label wk:reviewed") || !f.gh.Called("- 小さな点") {
		t.Fatal(f.gh.Calls())
	}
	e, _ := load()
	if approved(e, "Tester/alpha", 5) != sha {
		t.Fatal("approve の SHA が記録されていない")
	}
}

func TestReviewRequestChanges(t *testing.T) {
	f := reviewFactory(t)
	fakeClaude(t, `{"verdict":"request_changes","issues":[],"summary":"直す"}`, 0, "0")
	if _, err := capture(t, func() error { return cmdReview([]string{"Tester/alpha", "5"}) }); err != nil {
		t.Fatal(err)
	}
	if !f.gh.Called("--remove-label wk:reviewed") {
		t.Fatal(f.gh.Calls())
	}
}

func TestReviewErrors(t *testing.T) {
	ok := `{"verdict":"approve","issues":[],"summary":"s"}`
	cases := map[string]struct {
		args   []string
		rules  []fakegh.Rule
		claude string
		code   int
		setup  func(f *factory)
		want   string
	}{
		"引数が足りない":          {args: []string{"Tester/alpha"}, want: "使い方"},
		"PR 番号が数でない":       {args: []string{"Tester/alpha", "x"}, want: "expected integer"},
		"portfolio にない":    {args: []string{"Other/x", "5"}, want: "portfolio にない"},
		"先頭が読めない":          {rules: []fakegh.Rule{{Match: `headRefOid`, Code: 1}}, want: "headRefOid"},
		"先頭が短い":            {rules: []fakegh.Rule{{Match: `headRefOid`, Out: "abc"}}, want: "読めない"},
		"差分が読めない":          {rules: []fakegh.Rule{{Match: `^pr diff`, Code: 1}}, want: "pr diff"},
		"差分が大きすぎる":         {rules: []fakegh.Rule{{Match: `^pr diff`, Out: strings.Repeat("x", maxDiffBytes+1)}}, want: "大きすぎる"},
		"PR が読めない":         {rules: []fakegh.Rule{{Match: `--json title`, Code: 1}}, want: "title"},
		"PR の JSON が壊れている": {rules: []fakegh.Rule{{Match: `--json title`, Out: "{"}}, want: "JSON"},
		"Closes がない":       {rules: []fakegh.Rule{{Match: `--json title`, Out: `{"title":"t"}`}}, want: "Closes"},
		"issue が読めない":      {rules: []fakegh.Rule{{Match: `^issue view`, Code: 1}}, want: "issue view"},
		"reviewer の指示がない":  {setup: func(f *factory) { os.Remove(filepath.Join(f.root, "harness", "reviewers", "engineer.md")) }, want: "engineer.md"},
		"変えたファイルが読めない":     {rules: []fakegh.Rule{{Match: `--json files`, Code: 1}}, want: "files"},
		"claude が失敗":       {claude: ok, code: 1, want: "claude -p"},
		"claude の作業場所がない":  {setup: func(f *factory) { must(t, os.RemoveAll(f.runtime)); f.write(f.runtime, "") }, want: "runtime"},
		"出力が JSON でない":     {claude: "わかりません", want: "JSON の形でない"},
		"結論が不正":            {claude: `{"verdict":"maybe"}`, want: "JSON の形でない"},
		"コメントできない":         {rules: []fakegh.Rule{{Match: `^pr comment`, Code: 1}}, want: "pr comment"},
		"approve を記録できない":  {setup: func(f *factory) { f.block(filepath.Join(f.runtime, "approvals.json")) }, want: "approvals.json"},
		"ラベルを付けられない":       {rules: []fakegh.Rule{{Match: `--add-label`, Code: 1}}, want: "add-label"},
		"設定が壊れている":         {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := reviewFactory(t, c.rules...)
			out := c.claude
			if out == "" {
				out = ok
			}
			fakeClaude(t, out, c.code, "0")
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"Tester/alpha", "5"}
			}
			_, err := capture(t, func() error { return cmdReview(args) })
			errHas(t, err, c.want)
		})
	}
}

func TestRunClaudeTimeout(t *testing.T) {
	newFactory(t)
	fakeClaude(t, "{}", 0, "5")
	old := claudeTimeout
	claudeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { claudeTimeout = old })
	e, _ := load()
	if _, err := runClaude(e, "x"); err == nil {
		t.Fatal("時間切れにならない")
	}
}

func TestExtractJSON(t *testing.T) {
	for in, want := range map[string]string{"a {\"x\":1} b": `{"x":1}`, "なし": "なし", "} {": "} {"} {
		if got := extractJSON(in); got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
}

// ---- merge ----

func fastMerge(t *testing.T) {
	t.Helper()
	p, w, n := mergePoll, mergeWait, mergeNoCI
	mergePoll, mergeWait, mergeNoCI = time.Millisecond, time.Minute, time.Minute
	t.Cleanup(func() { mergePoll, mergeWait, mergeNoCI = p, w, n })
}

func mergeFactory(t *testing.T, extra ...fakegh.Rule) *factory {
	fastMerge(t)
	f := newFactory(t, append(extra,
		fakegh.Rule{Match: `^pr view 5 --repo Tester/alpha --json labels`, Out: `["wk:reviewed"]`},
		fakegh.Rule{Match: `^pr view 5 --repo Tester/alpha --json headRefOid`, Out: sha},
		fakegh.Rule{Match: `^pr view 5 --repo Tester/alpha --json files`, Out: "src/a.ts\ntests/a.test.ts\n"},
		fakegh.Rule{Match: `^pr checks 5`, Out: `[{"name":"verify","bucket":"pass"}]`},
	)...)
	f.projects(active())
	e, _ := load()
	must(t, os.MkdirAll(f.runtime, 0o755))
	must(t, recordApproval(e, "Tester/alpha", 5, sha))
	return f
}

func TestMergeWaitsForPendingThenMerges(t *testing.T) {
	f := mergeFactory(t, fakegh.Rule{Match: `^pr checks 5`, Out: `[{"name":"verify","bucket":"pending"}]`, Code: 8, Times: 2})
	out, err := capture(t, func() error { return cmdMerge([]string{"Tester/alpha", "5"}) })
	if err != nil || !strings.Contains(out, "merge した") || !f.gh.Called("pr merge 5 --repo Tester/alpha --squash --delete-branch") {
		t.Fatal(out, err, f.gh.Calls())
	}
}

func TestMergeErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		rules []fakegh.Rule
		setup func(f *factory)
		want  string
	}{
		"引数が違う":          {args: []string{"x"}, want: "使い方"},
		"portfolio にない":  {args: []string{"Other/x", "5"}, want: "portfolio にない"},
		"ラベルが読めない":       {rules: []fakegh.Rule{{Match: `--json labels`, Code: 1}}, want: "labels"},
		"ラベルがない":         {rules: []fakegh.Rule{{Match: `--json labels`, Out: `[]`}}, want: "wk:reviewed がない"},
		"先頭が読めない":        {rules: []fakegh.Rule{{Match: `headRefOid`, Code: 1}}, want: "headRefOid"},
		"approve されていない": {rules: []fakegh.Rule{{Match: `headRefOid`, Out: strings.Repeat("f", 40)}}, want: "approve されていない"},
		"変更が読めない":        {rules: []fakegh.Rule{{Match: `--json files`, Code: 1}}, want: "files"},
		"工場が変えられない守りの部分": {rules: []fakegh.Rule{{Match: `--json files`, Out: ".github/workflows/ci.yml\n.claude/settings.json\nwk.json\n"}}, want: "(人だけ): .claude/settings.json, wk.json"},
		"CI が読めない":       {rules: []fakegh.Rule{{Match: `^pr checks`, Code: 1}}, want: "pr checks"},
		"CI が赤":          {rules: []fakegh.Rule{{Match: `^pr checks`, Out: `[{"name":"mutation","bucket":"fail"},{"name":"e2e","bucket":"cancel"}]`}}, want: "mutation, e2e"},
		"CI が動いていない":     {rules: []fakegh.Rule{{Match: `^pr checks`, Out: `[]`}}, setup: func(*factory) { mergeNoCI = 0 }, want: "1 つも動いていない"},
		"CI が終わらない":      {rules: []fakegh.Rule{{Match: `^pr checks`, Out: `[{"name":"v","bucket":"pending"}]`}}, setup: func(*factory) { mergeWait = 0 }, want: "終わらない"},
		"merge できない":     {rules: []fakegh.Rule{{Match: `^pr merge`, Code: 1}}, want: "pr merge"},
		"設定が壊れている":       {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := mergeFactory(t, c.rules...)
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"Tester/alpha", "5"}
			}
			_, err := capture(t, func() error { return cmdMerge(args) })
			errHas(t, err, c.want)
		})
	}
}

// ---- フック ----

// stdin は標準入力を s にする。
func stdin(t *testing.T, s string) {
	t.Helper()
	r, w, err := os.Pipe()
	must(t, err)
	go func() { w.WriteString(s); w.Close() }()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
}

func hook(cwd, key, value string) string {
	return `{"tool_name":"x","cwd":"` + cwd + `","tool_input":{"` + key + `":"` + value + `"}}`
}

func TestGuardHooksInFactoryAndApp(t *testing.T) {
	f := newFactory(t)
	f.projects(active())
	// 工場の中
	stdin(t, hook(f.root, "command", "ls"))
	must(t, cmdGuardBash(nil))
	stdin(t, hook(f.root, "command", "gh repo delete Tester/ideas --yes"))
	if code(cmdGuardBash(nil)) != 2 {
		t.Fatal("止めない")
	}
	stdin(t, hook(f.root, "file_path", "factory.json"))
	if code(cmdGuardEdit(nil)) != 2 {
		t.Fatal("factory.json の書き換えを止めない")
	}
	stdin(t, hook(f.root, "file_path", "notes.md"))
	must(t, cmdGuardEdit(nil))
	// 変えてよい守りの部分 (reviewable_paths) は書ける (wk sync の守りのレビューが見る)
	f.writeConfig(map[string]any{"protected_paths": []string{"factory.json", "templates/"}, "reviewable_paths": []string{"templates/"}})
	stdin(t, hook(f.root, "file_path", "templates/x.yml"))
	must(t, cmdGuardEdit(nil))
	stdin(t, hook(f.root, "file_path", "factory.json"))
	if code(cmdGuardEdit(nil)) != 2 {
		t.Fatal("reviewable でない守りの部分を止めない")
	}
	// アプリの repo の中: 守るのはアプリの守りの部分
	app := filepath.Join(f.apps, "alpha")
	must(t, os.MkdirAll(app, 0o755))
	_, err := runGit(app, "init", "-q")
	must(t, err)
	t.Setenv("WK_FACTORY", f.root)
	stdin(t, hook(app, "file_path", filepath.Join(app, ".claude/settings.json")))
	if code(cmdGuardEdit(nil)) != 2 {
		t.Fatal("アプリのフックの書き換えを止めない")
	}
	// CI は守りのレビュー (wk review の guardian) を通せば変えてよい
	stdin(t, hook(app, "file_path", filepath.Join(app, ".github/workflows/ci.yml")))
	must(t, cmdGuardEdit(nil))
	stdin(t, hook(app, "file_path", filepath.Join(app, "factory.json"))) // アプリでは守りの部分でない
	must(t, cmdGuardEdit(nil))
	// 工場の別の worktree (factory.json がある git の repo) は工場として扱う
	wt := t.TempDir()
	_, err = runGit(wt, "init", "-q")
	must(t, err)
	must(t, os.WriteFile(filepath.Join(wt, "factory.json"), []byte(f.read("factory.json")), 0o644))
	stdin(t, hook(wt, "file_path", filepath.Join(wt, "factory.json")))
	if code(cmdGuardEdit(nil)) != 2 {
		t.Fatal("工場の worktree の factory.json を止めない")
	}
}

func TestGuardHooksFailClosed(t *testing.T) {
	f := newFactory(t)
	stdin(t, "{")
	if code(cmdGuardBash(nil)) != 2 {
		t.Fatal("壊れた入力を通した")
	}
	stdin(t, "{")
	if code(cmdGuardEdit(nil)) != 2 {
		t.Fatal("壊れた入力を通した")
	}
	// 標準入力が読めない
	wonly, err := os.OpenFile(filepath.Join(t.TempDir(), "w"), os.O_CREATE|os.O_WRONLY, 0o644)
	must(t, err)
	old := os.Stdin
	os.Stdin = wonly
	err = cmdGuardBash(nil)
	os.Stdin = old
	if code(err) != 2 {
		t.Fatal("読めないのに通した")
	}
	// 工場が読めない
	f.write("factory.json", "{")
	stdin(t, hook(t.TempDir(), "command", "ls"))
	if code(cmdGuardBash(nil)) != 2 {
		t.Fatal("工場が読めないのに通した")
	}
}

// fakeClaudeBy は、プロンプトの先頭の「# reviewer:<ロール>」ごとに違う答えを返す偽物の claude を置く。
// 呼ばれた順は calls に残る。
func fakeClaudeBy(t *testing.T, answers map[string]string) (calls string) {
	t.Helper()
	d := t.TempDir()
	calls = filepath.Join(d, "calls")
	sh := "#!/bin/sh\ncat > " + filepath.Join(d, "in") + "\n"
	for r, out := range answers {
		must(t, os.WriteFile(filepath.Join(d, r), []byte(out), 0o644))
		sh += "if head -1 " + filepath.Join(d, "in") + " | grep -qx '# reviewer:" + r + "'; then echo " + r + " >> " + calls + "; cat " + filepath.Join(d, r) + "; exit 0; fi\n"
	}
	sh += "exit 1\n"
	must(t, os.WriteFile(filepath.Join(d, "claude"), []byte(sh), 0o755))
	t.Setenv("PATH", d+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
}

const (
	approveJSON = `{"verdict":"approve","issues":[],"summary":"よい"}`
	changesJSON = `{"role":"leader","verdict":"request_changes","issues":["直す所"],"summary":"直す"}`
)

// ロールごとのレビュー: UI に触れていなければ SRE → engineer の 2 人。全員 approve で記録される。
func TestReviewRolesNoUI(t *testing.T) {
	f := reviewFactory(t, fakegh.Rule{Match: `--json files`, Out: "internal/app/a.go\nREADME.md\n"})
	calls := fakeClaudeBy(t, map[string]string{"sre": approveJSON, "engineer": approveJSON, "designer": changesJSON})
	out, err := capture(t, func() error { return cmdReview([]string{"Tester/alpha", "5"}) })
	if err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, calls)); got != "sre\nengineer\n" {
		t.Fatalf("レビューの順: %q", got)
	}
	if !strings.Contains(out, `"verdict": "approve"`) || !strings.Contains(out, `"role": "sre"`) || !strings.Contains(out, `"role": "engineer"`) ||
		strings.Contains(out, `"role": "designer"`) {
		t.Fatal(out)
	}
	if !f.gh.Called("wakuwaku reviewer (sre): approve") || !f.gh.Called("wakuwaku reviewer (engineer): approve") || !f.gh.Called("--add-label wk:reviewed") {
		t.Fatal(f.gh.Calls())
	}
	e, _ := load()
	if approved(e, "Tester/alpha", 5) != sha {
		t.Fatal("approve の SHA が記録されていない")
	}
}

// UI に触れたら designer も見る。1 人でも request_changes なら通らないが、全員の指摘は出る。
func TestReviewRolesUI(t *testing.T) {
	for name, c := range map[string]struct {
		answers map[string]string
		ok      bool
	}{
		"全員 approve":  {map[string]string{"sre": approveJSON, "engineer": approveJSON, "designer": approveJSON}, true},
		"designer だけ": {map[string]string{"sre": approveJSON, "engineer": approveJSON, "designer": changesJSON}, false},
		"SRE だけ":      {map[string]string{"sre": changesJSON, "engineer": approveJSON, "designer": approveJSON}, false},
		"engineer だけ": {map[string]string{"sre": approveJSON, "engineer": changesJSON, "designer": approveJSON}, false},
	} {
		t.Run(name, func(t *testing.T) {
			f := reviewFactory(t, fakegh.Rule{Match: `--json files`, Out: "src/app.tsx\n"})
			calls := fakeClaudeBy(t, c.answers)
			out, err := capture(t, func() error { return cmdReview([]string{"Tester/alpha", "5"}) })
			if err != nil {
				t.Fatal(err)
			}
			// 途中で request_changes が出ても、全員が見る
			if got := string(mustRead(t, calls)); got != "sre\nengineer\ndesigner\n" {
				t.Fatalf("レビューの順: %q", got)
			}
			e, _ := load()
			if c.ok {
				if !strings.Contains(out, "{\n  \"verdict\": \"approve\"") || !f.gh.Called("--add-label wk:reviewed") || approved(e, "Tester/alpha", 5) != sha {
					t.Fatal(out, f.gh.Calls())
				}
				return
			}
			if !strings.Contains(out, "{\n  \"verdict\": \"request_changes\"") || f.gh.Called("--add-label wk:reviewed") ||
				!f.gh.Called("--remove-label wk:reviewed") || approved(e, "Tester/alpha", 5) != "" {
				t.Fatal(out, f.gh.Calls())
			}
			// 出力に書かれた role (leader) は信じず、割り当てたロールで記録する
			if strings.Contains(out, `"role": "leader"`) || !f.gh.Called("- 直す所") {
				t.Fatal(out)
			}
		})
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	return b
}
