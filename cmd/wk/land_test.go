package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/role"
)

const greenRuns = `[{"name":"verify","status":"completed","conclusion":"success"},{"name":"e2e","status":"completed","conclusion":"skipped"}]`

func landRules(extra ...fakegh.Rule) []fakegh.Rule {
	return append(extra,
		fakegh.Rule{Match: `^api repos/Tester/alpha/commits/[0-9a-f]+/check-runs`, Out: greenRuns},
		fakegh.Rule{Match: `^issue view 11 --repo Tester/alpha --json number,title,body`, Out: "#11 one\nファイルを作る"},
		fakegh.Rule{Match: `^issue comment 11 --repo Tester/alpha`},
		fakegh.Rule{Match: `^issue close 11 --repo Tester/alpha`},
	)
}

// landFactory は task #11 を作って wk verify が通り、wk/t11 を push したところ (tick の中)。
func landFactory(t *testing.T, extra ...fakegh.Rule) (*v4fix, string) {
	t.Helper()
	fastMerge(t)
	v := v4Factory(t, landRules(extra...)...)
	v.runPlan(t)
	wt := v.taskWorktree(t, "11")
	v.write(filepath.Join(wt, "src", "one.txt"), "one\n")
	for _, c := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "one (#11)"}} {
		if out, err := runGit(wt, c...); err != nil {
			t.Fatal(out, err)
		}
	}
	if out, err := capture(t, func() error { return cmdVerify([]string{"11"}) }); err != nil {
		t.Fatal(out, err)
	}
	if out, err := runGit(wt, "push", "-q", "origin", "wk/t11"); err != nil {
		t.Fatal(out, err)
	}
	must(t, writeCurrent(v.env(t), nextActionIssue(11)))
	for _, r := range append(role.Reviewers([]string{"src/app.tsx"}), guardianReviewer) {
		v.write("harness/reviewers/"+r+".md", "# reviewer:"+r+"\n")
	}
	fakeClaude(t, `{"verdict":"approve","summary":"よい"}`, 0, "0")
	head, _ := runGit(wt, "rev-parse", "HEAD")
	return v, strings.TrimSpace(head)
}

func TestLand(t *testing.T) {
	v, head := landFactory(t)
	out, err := capture(t, func() error { return cmdLand([]string{"11"}) })
	if err != nil || !strings.Contains(out, "main に入れた: Tester/alpha wk/t11") {
		t.Fatal(out, err)
	}
	bare := filepath.Join(v.remote, "Tester", "alpha.git")
	if got, _ := runGit(bare, "rev-parse", "main"); strings.TrimSpace(got) != head {
		t.Fatal(got, head)
	}
	if _, err := runGit(bare, "rev-parse", "--verify", "-q", "refs/heads/wk/t11"); err == nil {
		t.Fatal("ブランチが残っている")
	}
	if !v.gh.Called("issue close 11 --repo Tester/alpha --comment wakuwaku factory: wk/t11 ("+head[:8]+") を main に入れた") ||
		!v.gh.Called("issue comment 11 --repo Tester/alpha --body **wakuwaku reviewer") || v.gh.Called("pr ") {
		t.Fatal(v.gh.Calls())
	}
}

func TestLandCleanupFailuresDoNotFail(t *testing.T) {
	_, _ = landFactory(t, fakegh.Rule{Match: `^issue close 11`, Code: 1})
	failV4Git(t, "--delete")
	out, err := capture(t, func() error { return cmdLand([]string{"11"}) })
	if err != nil || !strings.Contains(out, "main に入れた") {
		t.Fatal(out, err)
	}
}

func TestLandRequestChanges(t *testing.T) {
	v, _ := landFactory(t)
	fakeClaude(t, `{"verdict":"request_changes","summary":"直す","issues":["名前"]}`, 0, "0")
	out, err := capture(t, func() error { return cmdLand([]string{"11"}) })
	errHas(t, err, "request_changes")
	if !strings.Contains(out, `"verdict": "request_changes"`) {
		t.Fatal(out)
	}
	if got, _ := runGit(filepath.Join(v.remote, "Tester", "alpha.git"), "rev-parse", "--verify", "-q", "refs/heads/wk/t11"); got == "" {
		t.Fatal("入れていないのにブランチを消した")
	}
}

func TestLandErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		rules []fakegh.Rule
		setup func(t *testing.T, v *v4fix, head string)
		want  string
	}{
		"引数がない":            {args: []string{}, want: "使い方"},
		"知らない flag":        {args: []string{"-x"}, want: "-x"},
		"番号が違う":            {args: []string{"x"}, want: "番号が違う"},
		"tick の外":          {setup: func(t *testing.T, v *v4fix, _ string) { must(t, os.Remove(currentFile(t, v))) }, want: "task の tick の中"},
		"ほかの task":         {setup: func(t *testing.T, v *v4fix, _ string) { must(t, writeCurrent(v.env(t), nextActionIssue(12))) }, want: "その task (#11)"},
		"fetch できない":       {setup: func(t *testing.T, _ *v4fix, _ string) { failV4Git(t, "fetch") }, want: "fetch"},
		"先頭が読めない":          {setup: func(t *testing.T, _ *v4fix, _ string) { failV4Git(t, "rev-parse") }, want: "rev-parse"},
		"verify していない":     {setup: func(t *testing.T, v *v4fix, _ string) { must(t, os.Remove(verifiedPath(v.env(t)))) }, want: "wk verify が通っていない"},
		"main の上にない":       {setup: advanceMain, want: "main の最新の上にない"},
		"変更が読めない":          {setup: func(t *testing.T, _ *v4fix, _ string) { failV4Git(t, "--name-only") }, want: "--name-only"},
		"守りの部分":            {setup: touchProtected, want: "(人だけ): wk.json"},
		"差分が読めない":          {setup: func(t *testing.T, _ *v4fix, _ string) { failV4Git(t, "diff origin/main...") }, want: "diff origin/main..."},
		"差分が大きすぎる":         {setup: func(t *testing.T, _ *v4fix, _ string) { maxDiffBytesForTest(t, 1) }, want: "大きすぎる"},
		"CI が読めない":         {rules: []fakegh.Rule{{Match: `check-runs`, Code: 1}}, want: "check-runs"},
		"CI が赤":            {rules: []fakegh.Rule{{Match: `check-runs`, Out: `[{"name":"verify","status":"completed","conclusion":"failure"}]`}}, want: "CI が赤: verify"},
		"issue が読めない":      {rules: []fakegh.Rule{{Match: `^issue view 11`, Code: 1}}, want: "issue view 11"},
		"reviewer の文を残せない": {rules: []fakegh.Rule{{Match: `^issue comment 11`, Code: 1}}, want: "issue comment"},
		"main に push できない": {setup: func(t *testing.T, _ *v4fix, _ string) { failV4Git(t, "refs/heads/main") }, want: "git が落ちた"},
		"設定が壊れている":         {setup: func(_ *testing.T, v *v4fix, _ string) { v.write("factory.json", "{") }, want: "factory.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			v, head := landFactory(t, c.rules...)
			if c.setup != nil {
				c.setup(t, v, head)
			}
			args := c.args
			if args == nil {
				args = []string{"11"}
			}
			_, err := capture(t, func() error { return cmdLand(args) })
			errHas(t, err, c.want)
		})
	}
}

func currentFile(t *testing.T, v *v4fix) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(v.runtime, "current-*.json"))
	if len(m) != 1 {
		t.Fatal(m)
	}
	return m[0]
}

// advanceMain は、ほかの task が先に main に入ったところにする。
func advanceMain(t *testing.T, v *v4fix, _ string) {
	t.Helper()
	bare := filepath.Join(v.remote, "Tester", "alpha.git")
	tree, _ := runGit(bare, "rev-parse", "main^{tree}")
	c, err := runGit(bare, "commit-tree", "-p", "main", "-m", "other", strings.TrimSpace(tree))
	must(t, err)
	_, err = runGit(bare, "update-ref", "refs/heads/main", strings.TrimSpace(c))
	must(t, err)
}

// touchProtected は、守りの部分 (wk.json) を変えたコミットを verify 済みとして push したところにする。
func touchProtected(t *testing.T, v *v4fix, _ string) {
	t.Helper()
	wt, _ := os.Getwd()
	v.write(filepath.Join(wt, "wk.json"), "{}\n")
	for _, c := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "x"}, {"push", "-q", "origin", "wk/t11"}} {
		if out, err := runGit(wt, c...); err != nil {
			t.Fatal(out, err)
		}
	}
	head, _ := runGit(wt, "rev-parse", "HEAD")
	b, _ := json.Marshal(map[string]string{"Tester/alpha#11": strings.TrimSpace(head)})
	must(t, os.WriteFile(verifiedPath(v.env(t)), b, 0o600))
}

func maxDiffBytesForTest(t *testing.T, n int) {
	old := maxDiffBytes
	maxDiffBytes = n
	t.Cleanup(func() { maxDiffBytes = old })
}
