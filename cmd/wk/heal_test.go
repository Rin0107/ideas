package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/guard"
	"github.com/Rin0107/ideas/internal/lock"
	"github.com/Rin0107/ideas/internal/next"
	"github.com/Rin0107/ideas/internal/state"
)

// startAction は a の tick を始めた状態にする。
func startAction(t *testing.T, f *factory, a next.Action) {
	t.Helper()
	e, err := load()
	must(t, err)
	_, err = lock.Acquire(f.runtime, "factory", lock.Info{Since: time.Now(), Phase: a.Phase}, time.Hour)
	must(t, err)
	if a.Reason == "" {
		a.Reason = "テスト"
	}
	must(t, writeCurrent(e, a))
}

// doneWith は wk done を呼び、標準エラーを返す (gh の失敗は標準エラーに出て、tick は終わる)。
func doneWith(t *testing.T, f *factory, args ...string) string {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	_, err := capture(t, func() error { return cmdDone(args) })
	os.Stderr = old
	w.Close()
	b := make([]byte, 8192)
	n, _ := r.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.runtime, "tick-factory.lock")); err == nil {
		t.Fatal("ロックが残った")
	}
	return string(b[:n])
}

func healFactory(t *testing.T, rules ...fakegh.Rule) *factory { return newFactory(t, rules...) }

func failures(t *testing.T, f *factory) int {
	t.Helper()
	rt, err := state.LoadRuntime(f.runtime, time.Now())
	must(t, err)
	return rt.ConsecutiveFailures
}

func hasCall(t *testing.T, f *factory, parts ...string) {
	t.Helper()
	// 本文に改行があると、記録では 1 回の呼び出しが何行にも分かれる。最初の部分が合った行から後ろに、残りが全部あればよい
	log := strings.Join(f.gh.Calls(), "\n")
	for i := 0; ; {
		j := strings.Index(log[i:], parts[0])
		if j < 0 {
			break
		}
		rest, ok := log[i+j:], true
		if k := strings.Index(rest[1:], "\nissue ") + 1; k > 0 {
			rest = rest[:k]
		}
		for _, p := range parts[1:] {
			ok = ok && strings.Contains(rest, p)
		}
		if ok {
			return
		}
		i += j + 1
	}
	t.Fatalf("%q を含む呼び出しがない:\n%s", parts, log)
}

func noCall(t *testing.T, f *factory, sub string) {
	t.Helper()
	if f.gh.Called(sub) {
		t.Fatalf("%q を呼んだ:\n%s", sub, strings.Join(f.gh.Calls(), "\n"))
	}
}

func TestDoneFixInAppOpensIssueInMilestone(t *testing.T) {
	f := healFactory(t)
	f.projects(active())
	detail := filepath.Join(t.TempDir(), "d.md")
	must(t, os.WriteFile(detail, []byte("  CI の権限が足りない\n"), 0o644))
	startAction(t, f, next.Action{Phase: state.PhaseBuild, Repo: "Tester/alpha", Issue: 3, Role: "designer"})
	if out := doneWith(t, f, "--result", "fix", "--note", "gitleaks が 403", "--detail", detail); out != "" {
		t.Fatal(out)
	}
	if failures(t, f) != 0 {
		t.Fatal("fix を失敗に数えた")
	}
	hasCall(t, f, "label create wk:fix --repo Tester/alpha --color 0e8a16 --force")
	hasCall(t, f, "issue list --repo Tester/alpha --milestone MVP")
	hasCall(t, f, "issue create --repo Tester/alpha --title wk:fix gitleaks が 403 --label wk:fix --label role:designer",
		"tick: build (担当 designer) Tester/alpha #3", "CI の権限が足りない --milestone MVP")
	noCall(t, f, "wk:human")
	ts, _ := state.ReadTicks(f.root)
	if len(ts) != 1 || ts[0].Result != "fix" {
		t.Fatalf("%+v", ts)
	}
}

func TestDoneFixKeepsFailureCount(t *testing.T) {
	f := healFactory(t)
	f.write(filepath.Join(f.runtime, "runtime.json"), `{"day":"`+time.Now().Format(time.DateOnly)+`","consecutive_failures":2}`)
	startAction(t, f, next.Action{Phase: state.PhaseSelect, Role: "leader"})
	doneWith(t, f, "--result", "fix", "--note", "x")
	if failures(t, f) != 2 {
		t.Fatal("fix で失敗の数が変わった")
	}
	if _, err := os.Stat(filepath.Join(f.runtime, "STOP")); err == nil {
		t.Fatal("fix で止まった")
	}
}

func TestDoneFixGoesToFactory(t *testing.T) {
	for name, c := range map[string]struct {
		a  next.Action
		in string
	}{
		"アプリでない段":      {next.Action{Phase: state.PhaseSelect, Role: "leader"}, ""},
		"--in factory": {next.Action{Phase: state.PhaseBuild, Repo: "Tester/alpha", Issue: 3, Role: "engineer"}, "factory"},
		"ほかの repo":     {next.Action{Phase: state.PhaseBuild, Repo: "Tester/other", Issue: 3, Role: "engineer"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			f := healFactory(t)
			f.projects(active())
			startAction(t, f, c.a)
			args := []string{"--result", "fix", "--note", "壊れた"}
			if c.in != "" {
				args = append(args, "--in", c.in)
			}
			doneWith(t, f, args...)
			hasCall(t, f, "issue list --repo Tester/factory --label wk:fix --state open")
			hasCall(t, f, "issue create --repo Tester/factory --title wk:fix 壊れた --label wk:fix")
			noCall(t, f, "--milestone MVP")
		})
	}
}

func TestDoneFixNoActiveProject(t *testing.T) {
	f := healFactory(t)
	startAction(t, f, next.Action{Phase: state.PhaseBuild, Repo: "Tester/alpha", Issue: 1, Role: "sre"})
	doneWith(t, f, "--result", "fix", "--note", "y")
	hasCall(t, f, "issue create --repo Tester/factory --title wk:fix y --label wk:fix --label role:sre")
}

func TestDoneFixLeaderPhaseOwnedBySRE(t *testing.T) {
	f := healFactory(t)
	startAction(t, f, next.Action{Phase: state.PhaseEvaluate, Role: "leader"})
	doneWith(t, f, "--result", "fix", "--note", "z")
	hasCall(t, f, "--label wk:fix --label role:sre")
}

func TestDoneFixSameTitleCountsAttempt(t *testing.T) {
	f := healFactory(t, fakegh.Rule{Match: `^issue list --repo Tester/factory --label wk:fix`,
		Out: `[{"number":9,"title":"wk:fix 壊れた","labels":[{"name":"wk:fix"},{"name":"wk:attempt-1"}]},` +
			`{"number":4,"title":"wk:fix ほか","labels":[{"name":"wk:fix"}]}]`})
	startAction(t, f, next.Action{Phase: state.PhaseSelect, Role: "leader"})
	doneWith(t, f, "--result", "fix", "--note", "壊れた")
	noCall(t, f, "issue create")
	hasCall(t, f, "label create wk:attempt-2 --repo Tester/factory --color fbca04 --force")
	hasCall(t, f, "issue edit 9 --repo Tester/factory --add-label wk:attempt-2")
	noCall(t, f, "wk:blocked")
	hasCall(t, f, "issue comment 9 --repo Tester/factory --body wakuwaku factory: 直す試行 2/3 が通らなかった。")
	if failures(t, f) != 0 {
		t.Fatal("まだ上限でないのに失敗に数えた")
	}
}

func TestDoneFixIgnoresNonFixIssuesInMilestone(t *testing.T) {
	f := healFactory(t, fakegh.Rule{Match: `^issue list --repo Tester/alpha --milestone MVP`,
		Out: `[{"number":2,"title":"wk:fix 壊れた","labels":[{"name":"wk:task"}]}]`})
	f.projects(active())
	startAction(t, f, next.Action{Phase: state.PhaseBuild, Repo: "Tester/alpha", Issue: 2, Role: "engineer"})
	doneWith(t, f, "--result", "fix", "--note", "壊れた")
	hasCall(t, f, "issue create --repo Tester/alpha --title wk:fix 壊れた")
}

func TestDoneFixOnFixIssueEscalatesAtLimit(t *testing.T) {
	for name, c := range map[string]struct {
		labels   string
		escalate bool
	}{
		"まだ試せる": {`{"name":"wk:fix"}`, false},
		"上限":    {`{"name":"wk:fix"},{"name":"wk:attempt-2"}`, true},
	} {
		t.Run(name, func(t *testing.T) {
			f := healFactory(t, fakegh.Rule{Match: `^issue list --repo Tester/factory --label wk:fix`,
				Out: `[{"number":1,"title":"a","labels":[]},{"number":7,"title":"wk:fix 壊れた","labels":[` + c.labels + `]}]`})
			startAction(t, f, next.Action{Phase: state.PhaseRepair, Repo: "Tester/factory", Issue: 7, Fix: true, Role: "sre"})
			doneWith(t, f, "--result", "fix", "--note", "まだ落ちる")
			noCall(t, f, "issue create --repo Tester/factory --title wk:fix")
			if !c.escalate {
				hasCall(t, f, "issue edit 7 --repo Tester/factory --add-label wk:attempt-1")
				noCall(t, f, "wk:blocked")
				noCall(t, f, "wk:human")
				if failures(t, f) != 0 {
					t.Fatal("失敗に数えた")
				}
				return
			}
			hasCall(t, f, "issue edit 7 --repo Tester/factory --add-label wk:attempt-3,wk:blocked")
			hasCall(t, f, "issue create --repo Tester/factory --title wk:human 直す issue Tester/factory #7 が 3 回で直らない --label wk:human",
				"まだ落ちる")
			hasCall(t, f, "issue edit 7 --repo Tester/factory --add-label wk:blocked")
			if failures(t, f) != 1 {
				t.Fatal("人に上げたのに失敗に数えない")
			}
		})
	}
}

func TestDoneFixOnClosedFixIssue(t *testing.T) {
	f := healFactory(t)
	startAction(t, f, next.Action{Phase: state.PhaseRepair, Repo: "Tester/factory", Issue: 7, Fix: true, Role: "sre"})
	if out := doneWith(t, f, "--result", "fix", "--note", "x"); !strings.Contains(out, "Tester/factory #7 が open な wk:fix にない") {
		t.Fatal(out)
	}
}

func TestDoneHuman(t *testing.T) {
	f := healFactory(t)
	detail := filepath.Join(t.TempDir(), "d.md")
	must(t, os.WriteFile(detail, []byte("ci.yml に pull-requests: read を足す"), 0o644))
	startAction(t, f, next.Action{Phase: state.PhaseBuild, Repo: "Tester/alpha", Issue: 3, Role: "engineer"})
	doneWith(t, f, "--result", "human", "--note", "守りの部分の変更", "--detail", detail)
	if failures(t, f) != 1 {
		t.Fatal("human を失敗に数えない")
	}
	hasCall(t, f, "label create wk:human --repo Tester/factory --color d93f0b --force")
	hasCall(t, f, "issue create --repo Tester/factory --title wk:human 守りの部分の変更 --label wk:human --body 工場が人の作業を待っている。",
		"tick: build (担当 engineer) Tester/alpha #3", "pull-requests: read", "この issue を閉じる")
	hasCall(t, f, "issue edit 3 --repo Tester/alpha --add-label wk:blocked")
}

func TestDoneHumanWithoutIssue(t *testing.T) {
	f := healFactory(t)
	startAction(t, f, next.Action{Phase: state.PhaseSelect, Role: "leader"})
	doneWith(t, f, "--result", "human", "--note", "鍵が切れた")
	hasCall(t, f, "issue create --repo Tester/factory --title wk:human 鍵が切れた")
	noCall(t, f, "issue edit")
}

func TestDoneHumanIssueWithoutRepo(t *testing.T) {
	f := healFactory(t)
	startAction(t, f, next.Action{Phase: state.PhaseSelect, Issue: 4, Role: "leader"})
	doneWith(t, f, "--result", "human", "--note", "x")
	noCall(t, f, "issue edit")
}

func TestDoneHumanThirdTimeStops(t *testing.T) {
	f := healFactory(t)
	f.write(filepath.Join(f.runtime, "runtime.json"), `{"day":"`+time.Now().Format(time.DateOnly)+`","consecutive_failures":2}`)
	startAction(t, f, next.Action{Phase: state.PhaseSelect, Role: "leader"})
	doneWith(t, f, "--result", "human", "--note", "x")
	if _, err := os.Stat(filepath.Join(f.runtime, "STOP")); err != nil {
		t.Fatal("STOP がない")
	}
	hasCall(t, f, "issue create --repo Tester/factory --title wk:incident 連続失敗で停止", "--label wk:incident --label wk:human")
}

func TestDoneGhFailuresStillFinish(t *testing.T) {
	cases := map[string]struct {
		rule   string
		a      next.Action
		result string
		labels string
	}{
		"human のラベル":     {`^label create wk:human`, next.Action{Phase: state.PhaseSelect}, "human", ""},
		"human の issue":  {`^issue create`, next.Action{Phase: state.PhaseSelect}, "human", ""},
		"blocked を付ける":   {`^issue edit`, next.Action{Phase: state.PhaseBuild, Repo: "Tester/alpha", Issue: 3}, "human", ""},
		"fix の一覧":        {`^issue list`, next.Action{Phase: state.PhaseSelect}, "fix", ""},
		"fix のラベル":       {`^label create wk:fix`, next.Action{Phase: state.PhaseSelect}, "fix", ""},
		"fix の issue":    {`^issue create`, next.Action{Phase: state.PhaseSelect}, "fix", ""},
		"直す issue を読み直す": {`^issue list`, next.Action{Phase: state.PhaseRepair, Repo: "Tester/factory", Issue: 7, Fix: true}, "fix", ""},
		"試行のラベル":         {`^label create wk:attempt`, next.Action{Phase: state.PhaseRepair, Repo: "Tester/factory", Issue: 7, Fix: true}, "fix", `{"name":"wk:fix"}`},
		"試行を付ける":         {`^issue edit`, next.Action{Phase: state.PhaseRepair, Repo: "Tester/factory", Issue: 7, Fix: true}, "fix", `{"name":"wk:fix"}`},
		"試行のコメント":        {`^issue comment`, next.Action{Phase: state.PhaseRepair, Repo: "Tester/factory", Issue: 7, Fix: true}, "fix", `{"name":"wk:fix"}`},
		"上限で人に上げる":       {`^issue create`, next.Action{Phase: state.PhaseRepair, Repo: "Tester/factory", Issue: 7, Fix: true}, "fix", `{"name":"wk:fix"},{"name":"wk:attempt-2"}`},
		"上限の試行を付ける":      {`^issue edit`, next.Action{Phase: state.PhaseRepair, Repo: "Tester/factory", Issue: 7, Fix: true}, "fix", `{"name":"wk:fix"},{"name":"wk:attempt-2"}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rules := []fakegh.Rule{{Match: c.rule, Code: 1}}
			if c.labels != "" {
				rules = append(rules, fakegh.Rule{Match: `^issue list --repo Tester/factory --label wk:fix`,
					Out: `[{"number":7,"title":"t","labels":[` + c.labels + `]}]`})
			}
			f := healFactory(t, rules...)
			c.a.Role = "sre"
			startAction(t, f, c.a)
			if out := doneWith(t, f, "--result", c.result, "--note", "x"); !strings.Contains(out, "issue の作成に失敗") {
				t.Fatal(out)
			}
			want := 0
			if c.result == "human" || c.labels != "" && strings.Contains(c.labels, "attempt-2") && name != "上限の試行を付ける" {
				want = 1
			}
			if got := failures(t, f); got != want {
				t.Fatalf("失敗の数 %d (want %d)", got, want)
			}
		})
	}
}

func TestDoneBadDetail(t *testing.T) {
	f := healFactory(t)
	startAction(t, f, next.Action{Phase: state.PhaseSelect})
	_, err := capture(t, func() error {
		return cmdDone([]string{"--result", "human", "--detail", filepath.Join(f.root, "nope.md")})
	})
	errHas(t, err, "--detail を読めない")
}

func TestStopWithIncidentLabelFails(t *testing.T) {
	healFactory(t, fakegh.Rule{Match: `^label create`, Code: 1})
	e, err := load()
	must(t, err)
	if stopWithIncident(e, &state.Runtime{}, nextAction("build"), "") == nil {
		t.Fatal("ラベルを作れないのに成功")
	}
}

// ---- wk human ----

func TestHumanCommand(t *testing.T) {
	f := healFactory(t)
	detail := filepath.Join(t.TempDir(), "d.md")
	must(t, os.WriteFile(detail, []byte("Worker を消す"), 0o644))
	out, err := capture(t, func() error { return cmdHuman([]string{"--title", "drop した Worker の削除", "--detail", detail}) })
	if err != nil || !strings.Contains(out, "人に頼んだ: drop した Worker の削除") {
		t.Fatal(out, err)
	}
	hasCall(t, f, "issue create --repo Tester/factory --title wk:human drop した Worker の削除 --label wk:human --body Worker を消す")
	if failures(t, f) != 0 {
		t.Fatal("wk human を失敗に数えた")
	}
}

func TestHumanCommandErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		rules []fakegh.Rule
		brk   func(f *factory)
		want  string
	}{
		"知らない引数":   {args: []string{"--nope"}, want: "nope"},
		"題がない":     {args: nil, want: "--title が要る"},
		"本文を読めない":  {args: []string{"--title", "x", "--detail", "/nope/x.md"}, want: "--detail を読めない"},
		"設定が壊れている": {args: []string{"--title", "x"}, brk: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
		"gh が落ちる":  {args: []string{"--title", "x"}, rules: []fakegh.Rule{{Match: `^issue create`, Code: 1}}, want: "exit status 1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := healFactory(t, c.rules...)
			if c.brk != nil {
				c.brk(f)
			}
			_, err := capture(t, func() error { return cmdHuman(c.args) })
			errHas(t, err, c.want)
		})
	}
}

// ---- wk next: 工場の直す issue ----

func TestNextRepairsFactoryFirst(t *testing.T) {
	f := healFactory(t, fakegh.Rule{Match: `^issue list --repo Tester/factory --label wk:fix`,
		Out: `[{"number":5,"title":"wk:fix x","labels":[{"name":"wk:fix"},{"name":"role:sre"}]}]`})
	f.projects(active())
	out, err := capture(t, func() error { return cmdNext(nil) })
	if err != nil || !strings.Contains(out, `"phase": "repair"`) || !strings.Contains(out, `"repo": "Tester/factory"`) ||
		!strings.Contains(out, `"issue": 5`) || !strings.Contains(out, `"fix": true`) || !strings.Contains(out, `"role": "sre"`) {
		t.Fatal(out, err)
	}
	if f.portfolio().Active().Ticks.Total != 0 {
		t.Fatal("工場を直す tick をアプリの tick に数えた")
	}
}

func TestNextFactoryFixesUnreadable(t *testing.T) {
	healFactory(t, fakegh.Rule{Match: `^issue list --repo Tester/factory --label wk:fix`, Code: 1})
	_, err := capture(t, func() error { return cmdNext(nil) })
	errHas(t, err, "exit status 1")
}

func TestDoneRetry(t *testing.T) {
	f := healFactory(t)
	f.write(filepath.Join(f.runtime, "runtime.json"), `{"day":"`+time.Now().Format(time.DateOnly)+`","consecutive_failures":2}`)
	startAction(t, f, next.Action{Phase: state.PhaseBuild, Repo: "Tester/alpha", Issue: 3, Role: "engineer"})
	doneWith(t, f, "--result", "retry", "--note", "review が通らない")
	if failures(t, f) != 2 {
		t.Fatal("retry で失敗の数が変わった")
	}
	if len(f.gh.Calls()) != 0 {
		t.Fatal(f.gh.Calls())
	}
	if _, err := os.Stat(filepath.Join(f.runtime, "STOP")); err == nil {
		t.Fatal("retry で止まった")
	}
}

// ---- wk fix ----

func TestFixCommand(t *testing.T) {
	f := healFactory(t)
	detail := filepath.Join(t.TempDir(), "d.md")
	must(t, os.WriteFile(detail, []byte("手順書の誤り"), 0o644))
	out, err := capture(t, func() error { return cmdFix([]string{"--title", "patrol.md の誤り", "--detail", detail}) })
	if err != nil || !strings.Contains(out, "直す issue にした: patrol.md の誤り") {
		t.Fatal(out, err)
	}
	hasCall(t, f, "issue create --repo Tester/factory --title wk:fix patrol.md の誤り --label wk:fix --label role:sre",
		"wk fix から (tick の外)", "手順書の誤り")
}

func TestFixCommandErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		rules []fakegh.Rule
		brk   func(f *factory)
		want  string
	}{
		"知らない引数":   {args: []string{"--nope"}, want: "nope"},
		"題がない":     {args: nil, want: "--title が要る"},
		"本文を読めない":  {args: []string{"--title", "x", "--detail", "/nope/x.md"}, want: "--detail を読めない"},
		"設定が壊れている": {args: []string{"--title", "x"}, brk: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
		"gh が落ちる":  {args: []string{"--title", "x"}, rules: []fakegh.Rule{{Match: `^issue create`, Code: 1}}, want: "exit status 1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := healFactory(t, c.rules...)
			if c.brk != nil {
				c.brk(f)
			}
			_, err := capture(t, func() error { return cmdFix(c.args) })
			errHas(t, err, c.want)
		})
	}
}

// ---- 守りのレビュー (guardian) ----

func TestReviewAddsGuardianForProtected(t *testing.T) {
	f := reviewFactory(t, fakegh.Rule{Match: `--json files`, Out: "internal/app/a.go\n.github/workflows/ci.yml\n"})
	for name, c := range map[string]struct {
		guardian, verdict string
	}{
		"守りが approve": {approveJSON, `"verdict": "approve"`},
		"守りが通さない":     {changesJSON, `"verdict": "request_changes"`},
	} {
		t.Run(name, func(t *testing.T) {
			calls := fakeClaudeBy(t, map[string]string{"sre": approveJSON, "engineer": approveJSON, "guardian": c.guardian})
			out, err := capture(t, func() error { return cmdReview([]string{"Tester/alpha", "5"}) })
			if err != nil || !strings.Contains(out, c.verdict) {
				t.Fatal(out, err)
			}
			if got := string(mustRead(t, calls)); got != "sre\nengineer\nguardian\n" {
				t.Fatal(got)
			}
			hasCall(t, f, "pr comment 5 --repo Tester/alpha --body **wakuwaku reviewer (guardian)")
		})
	}
}

func TestReviewersFor(t *testing.T) {
	if got := reviewersFor([]string{"internal/a.go"}, nil); len(got) != 2 {
		t.Fatal(got)
	}
	if got := reviewersFor([]string{"spec/README.md"}, nil); len(got) != 3 || got[2] != guardianReviewer {
		t.Fatal(got)
	}
}

// syncWithProtected は、変えてよい守りの部分 (templates/) を変えたコミットを作業ブランチに置く。
func syncWithProtected(t *testing.T) (*factory, string) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"protected_paths": []string{"factory.json", "templates/", "harness/reviewers/"},
		"reviewable_paths": []string{"templates/"}})
	f.write("templates/ci.yml", "permissions: {}\n")
	f.write("harness/reviewers/guardian.md", "# reviewer:guardian\n")
	wt := syncRepos(t, f)
	t.Chdir(wt)
	must(t, os.WriteFile(filepath.Join(wt, "templates/ci.yml"), []byte("permissions: {pull-requests: read}\n"), 0o644))
	_, _ = runGit(wt, "commit", "-qam", "ci: 読む権限")
	return f, wt
}

func TestSyncGuardianApproves(t *testing.T) {
	f, _ := syncWithProtected(t)
	calls := fakeClaudeBy(t, map[string]string{"guardian": approveJSON})
	out, err := capture(t, func() error { return cmdSync(nil) })
	if err != nil || !strings.Contains(out, "守りのレビュー: approve — よい") || !strings.Contains(out, "main に merge して push した") {
		t.Fatal(out, err)
	}
	if got := string(mustRead(t, calls)); got != "guardian\n" {
		t.Fatal(got)
	}
	in := string(mustRead(t, filepath.Join(filepath.Dir(calls), "in")))
	if !strings.Contains(in, "+permissions: {pull-requests: read}") || !strings.Contains(in, "ci: 読む権限") {
		t.Fatal(in)
	}
	if out, _ := runGit(f.remote+"/Tester/factory.git", "log", "--oneline", "main"); !strings.Contains(out, "読む権限") {
		t.Fatal("push されていない", out)
	}
}

func TestSyncGuardianRefuses(t *testing.T) {
	cases := map[string]struct {
		claude string
		code   int
		brk    func(f *factory)
		want   string
	}{
		"通さない":       {claude: changesJSON, want: "守りのレビューが通らない (templates/ci.yml): 直す\n- 直す所"},
		"JSON でない":   {claude: "わからない", want: "守りのレビューの出力が JSON の形でない"},
		"結論が不正":      {claude: `{"verdict":"maybe"}`, want: "JSON の形でない"},
		"claude が失敗": {claude: approveJSON, code: 1, want: "claude -p"},
		"指示がない": {claude: approveJSON, brk: func(f *factory) {
			_, _ = runGit(f.root, "rm", "-q", "harness/reviewers/guardian.md")
			_, _ = runGit(f.root, "commit", "-qm", "rm")
		}, want: "guardian.md"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f, _ := syncWithProtected(t)
			fakeClaude(t, c.claude, c.code, "0")
			if c.brk != nil {
				c.brk(f)
			}
			_, err := capture(t, func() error { return cmdSync(nil) })
			errHas(t, err, c.want)
			if out, _ := runGit(f.remote+"/Tester/factory.git", "log", "--oneline", "main"); strings.Contains(out, "読む権限") {
				t.Fatal("通らないのに push した")
			}
		})
	}
}

func TestSyncRefusesHardProtected(t *testing.T) {
	f, wt := syncWithProtected(t)
	fakeClaude(t, approveJSON, 0, "0")
	must(t, os.WriteFile(filepath.Join(wt, "factory.json"), []byte(f.read("factory.json")+"\n"), 0o644))
	_, _ = runGit(wt, "commit", "-qam", "設定")
	errHas(t, cmdSync(nil), "人だけが変える): factory.json")
}

func TestCheckReviewableMustBeProtected(t *testing.T) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"reviewable_paths": []string{"templates/"}})
	_, err := capture(t, func() error { return cmdCheck(nil) })
	errHas(t, err, "reviewable_paths の templates/ が protected_paths にない")
}

// web-db: ログインとセッションは工場が変えられない。DB・写真・表は守りのレビューを通す (docs/DESIGN-v3.md §10)
func TestWebDBProtected(t *testing.T) {
	for _, p := range []string{"src/worker/db.ts", "src/worker/photo.ts", "migrations/0002_theme.sql", "src/worker/auth.ts"} {
		if got := reviewersFor([]string{p}, []string{"sre"}); got[len(got)-1] != guardianReviewer {
			t.Errorf("%s: %v", p, got)
		}
	}
	for _, p := range []string{"src/worker/auth.ts", "src/worker/session.ts"} {
		if hit := guard.TouchesProtected([]string{p}, appHard); len(hit) != 1 {
			t.Errorf("%s は変えられない: %v", p, hit)
		}
	}
	if hit := guard.TouchesProtected([]string{"src/worker/api.ts", "src/App.tsx"}, appHard); len(hit) != 0 {
		t.Errorf("機能の側は変えてよい: %v", hit)
	}
}
