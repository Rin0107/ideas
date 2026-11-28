package gh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rin0107/ideas/internal/fakegh"
)

func TestSetupAuth(t *testing.T) {
	d := t.TempDir()
	if _, err := Run(d, "git", "init", "-q"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // 何度やっても同じになる
		if err := SetupAuth(d); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Run(d, "git", "config", "--local", "--get-all", "credential.helper")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(strings.TrimRight(out, "\n"), "\n"); len(got) != 2 || got[0] != "" || got[1] != "!gh auth git-credential" {
		t.Fatalf("%q", got)
	}
}

// gitEnv は git の向き先を手元に変える: https://github.com/<repo>.git → <base>/<repo>.git (ネットワークに出ない)。
func gitEnv(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[user]\n\tname = t\n\temail = t@example.com\n[init]\n\tdefaultBranch = main\n[url \""+base+"/\"]\n\tinsteadOf = https://github.com/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	return base
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := Run(dir, "git", args...); err != nil {
		t.Fatal(err)
	}
}

// localRepo は main に 1 コミットある作業ディレクトリ。
func localRepo(t *testing.T) string {
	d := t.TempDir()
	gitRun(t, d, "init", "-q", "-b", "main")
	gitRun(t, d, "commit", "-q", "--allow-empty", "-m", "x")
	return d
}

func TestCreateAndPush(t *testing.T) {
	base := gitEnv(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(base, "o"), 0o755))
	gitRun(t, filepath.Join(base, "o"), "init", "-q", "--bare", "new.git")
	gitRun(t, filepath.Join(base, "o"), "init", "-q", "--bare", "empty.git")

	f := fakegh.Install(t,
		fakegh.Rule{Match: `^repo view o/new `, Code: 1},
		fakegh.Rule{Match: `^repo create o/new --public`},
		fakegh.Rule{Match: `^repo view o/(empty|full|nopush) `},
		fakegh.Rule{Match: `^api repos/o/full/commits`, Out: "abc"},
		fakegh.Rule{Match: `^api repos/o/(empty|nopush)/commits`, Code: 1},
		fakegh.Rule{Match: `^repo view o/denied `, Code: 1},
		fakegh.Rule{Match: `^repo create o/denied --private`, Code: 1},
	)
	// 新しく作って push する
	must(CreateAndPush("o/new", localRepo(t), "d", true))
	if !f.Called("repo create o/new --public --description d") {
		t.Fatal(f.Calls())
	}
	if out, _ := Run(filepath.Join(base, "o", "new.git"), "git", "log", "--oneline", "main"); out == "" {
		t.Fatal("push されていない")
	}
	// 前の試みで空の repo だけがある: push だけやり直す
	must(CreateAndPush("o/empty", localRepo(t), "d", true))
	// 中身がある repo には上書きしない
	if err := CreateAndPush("o/full", localRepo(t), "d", true); err == nil || !strings.Contains(err.Error(), "中身もある") {
		t.Fatal(err)
	}
	// 作れない
	if CreateAndPush("o/denied", localRepo(t), "d", false) == nil || !f.Called("repo create o/denied --private") {
		t.Fatal("作れないのに成功", f.Calls())
	}
	// git の repo でない (認証の設定ができない)
	if CreateAndPush("o/empty", t.TempDir(), "d", true) == nil {
		t.Fatal("repo でないのに成功")
	}
	// remote がすでにある
	d := localRepo(t)
	gitRun(t, d, "remote", "add", "origin", "x")
	if CreateAndPush("o/empty", d, "d", true) == nil {
		t.Fatal("remote があるのに成功")
	}
	// push 先がない
	if CreateAndPush("o/nopush", localRepo(t), "d", true) == nil {
		t.Fatal("push 先がないのに成功")
	}
}

func TestClone(t *testing.T) {
	base := gitEnv(t)
	src := localRepo(t)
	if err := os.MkdirAll(filepath.Join(base, "o"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, filepath.Join(base, "o"), "clone", "-q", "--bare", src, "site.git")
	dst := filepath.Join(t.TempDir(), "site")
	if err := Clone("o/site", dst); err != nil {
		t.Fatal(err)
	}
	if out, _ := Run(dst, "git", "config", "--local", "--get-all", "credential.helper"); !strings.Contains(out, "gh auth git-credential") {
		t.Fatal(out)
	}
	if Clone("o/missing", filepath.Join(t.TempDir(), "x")) == nil {
		t.Fatal("ない repo を clone できた")
	}
}

func TestCloneMirror(t *testing.T) {
	base := gitEnv(t)
	src := localRepo(t)
	if err := os.MkdirAll(filepath.Join(base, "o"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, filepath.Join(base, "o"), "clone", "-q", "--bare", src, "site.git")
	gitRun(t, src, "push", "-q", filepath.Join(base, "o", "site.git"), "HEAD:refs/pull/1/head")
	dst := filepath.Join(t.TempDir(), "site.git")
	if err := CloneMirror("o/site", dst); err != nil {
		t.Fatal(err)
	}
	if out, _ := Run(dst, "git", "for-each-ref", "--format=%(refname)"); !strings.Contains(out, "refs/pull/1/head") {
		t.Fatal(out)
	}
	if CloneMirror("o/missing", filepath.Join(t.TempDir(), "x")) == nil {
		t.Fatal("ない repo を clone できた")
	}
}

func TestSetupAuthOutsideRepo(t *testing.T) {
	gitEnv(t)
	if SetupAuth(t.TempDir()) == nil {
		t.Fatal("repo でないのに成功")
	}
}

func TestOpenIssues(t *testing.T) {
	fakegh.Install(t,
		fakegh.Rule{Match: `--repo o/ok `, Out: `[{"number":2,"title":"b","labels":[{"name":"wk:blocked"}]},{"number":1,"title":"a","labels":[]}]`},
		fakegh.Rule{Match: `--repo o/bad `, Out: `{`},
		fakegh.Rule{Match: `--repo o/err `, Code: 1},
	)
	is, err := OpenIssues("o/ok", "MVP")
	if err != nil || len(is) != 2 || !is[0].Blocked() || is[1].Blocked() || is[1].Title != "a" {
		t.Fatal(is, err)
	}
	if _, err := OpenIssues("o/bad", "MVP"); err == nil {
		t.Fatal("壊れた JSON を通した")
	}
	if _, err := OpenIssues("o/err", "MVP"); err == nil {
		t.Fatal("gh の失敗を通した")
	}
}

func TestChecks(t *testing.T) {
	fakegh.Install(t,
		fakegh.Rule{Match: `^pr checks 1 `, Out: `[{"name":"ci","bucket":"pass"}]`},
		fakegh.Rule{Match: `^pr checks 2 `, Out: `[{"name":"ci","bucket":"pending"}]`, Code: 8}, // 待ちでも JSON は読む
		fakegh.Rule{Match: `^pr checks 3 `, Code: 1},
		fakegh.Rule{Match: `^pr checks 4 `, Out: `nope`},
	)
	if cs, err := Checks("o/a", 1); err != nil || cs[0].Bucket != "pass" {
		t.Fatal(cs, err)
	}
	if cs, err := Checks("o/a", 2); err != nil || cs[0].Bucket != "pending" {
		t.Fatal(cs, err)
	}
	if _, err := Checks("o/a", 3); err == nil {
		t.Fatal("出力のない失敗を通した")
	}
	if _, err := Checks("o/a", 4); err == nil {
		t.Fatal("壊れた出力を通した")
	}
}

func TestLabels(t *testing.T) {
	fakegh.Install(t,
		fakegh.Rule{Match: `^pr view 1 `, Out: `["wk:reviewed","x"]`},
		fakegh.Rule{Match: `^pr view 2 `, Code: 1},
	)
	if ls, err := Labels("o/a", 1); err != nil || len(ls) != 2 || ls[0] != "wk:reviewed" {
		t.Fatal(ls, err)
	}
	if _, err := Labels("o/a", 2); err == nil {
		t.Fatal("失敗を通した")
	}
}

func TestOpenWithLabel(t *testing.T) {
	f := fakegh.Install(t,
		fakegh.Rule{Match: `--repo o/ok `, Out: `[{"number":7},{"number":3},{"number":5}]`},
		fakegh.Rule{Match: `--repo o/none `, Out: `[]`},
		fakegh.Rule{Match: `--repo o/bad `, Out: `{`},
		fakegh.Rule{Match: `--repo o/err `, Code: 1},
	)
	ns, err := OpenWithLabel("o/ok", "wk:proposal")
	if err != nil || len(ns) != 3 || ns[0] != 3 || ns[1] != 5 || ns[2] != 7 {
		t.Fatal(ns, err)
	}
	if !f.Called("issue list --repo o/ok --label wk:proposal --state open --limit 100 --json number") {
		t.Fatal(f.Calls())
	}
	if ns, err := OpenWithLabel("o/none", "x"); err != nil || len(ns) != 0 {
		t.Fatal(ns, err)
	}
	if _, err := OpenWithLabel("o/bad", "x"); err == nil || !strings.Contains(err.Error(), "読めない") {
		t.Fatal("壊れた JSON を通した", err)
	}
	if _, err := OpenWithLabel("o/err", "x"); err == nil {
		t.Fatal("gh の失敗を通した")
	}
}

func TestLabeledIssues(t *testing.T) {
	f := fakegh.Install(t,
		fakegh.Rule{Match: `^issue list --repo o/ok --label wk:fix --state open --limit 100 --json number,title,labels$`,
			Out: `[{"number":3,"title":"wk:fix a","labels":[{"name":"wk:fix"}]}]`},
		fakegh.Rule{Match: `--repo o/err `, Code: 1},
	)
	is, err := LabeledIssues("o/ok", "wk:fix")
	if err != nil || len(is) != 1 || !is[0].Fix() || is[0].Number != 3 || is[0].Title != "wk:fix a" {
		t.Fatal(is, err, f.Calls())
	}
	if _, err := LabeledIssues("o/err", "wk:fix"); err == nil {
		t.Fatal("gh の失敗を通した")
	}
}

func TestCommitChecksBuckets(t *testing.T) {
	fakegh.Install(t,
		fakegh.Rule{Match: `commits/a/check-runs`, Out: `[{"name":"a","status":"in_progress","conclusion":null},` +
			`{"name":"b","status":"completed","conclusion":"success"},{"name":"c","status":"completed","conclusion":"neutral"},` +
			`{"name":"d","status":"completed","conclusion":"skipped"},{"name":"e","status":"completed","conclusion":"cancelled"},` +
			`{"name":"f","status":"completed","conclusion":"timed_out"},{"name":"g","status":"queued","conclusion":null}]`},
		fakegh.Rule{Match: `commits/b/check-runs`, Out: `{`},
		fakegh.Rule{Match: `commits/c/check-runs`, Code: 1},
	)
	cs, err := CommitChecks("o/r", "a")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range cs {
		got = append(got, c.Name+"="+c.Bucket)
	}
	if strings.Join(got, " ") != "a=pending b=pass c=pass d=skipping e=cancel f=fail g=pending" {
		t.Fatal(got)
	}
	if _, err := CommitChecks("o/r", "b"); err == nil || !strings.Contains(err.Error(), "読めない") {
		t.Fatal(err)
	}
	if _, err := CommitChecks("o/r", "c"); err == nil {
		t.Fatal("読めないのに通った")
	}
}
