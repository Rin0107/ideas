package role

import (
	"slices"
	"testing"

	"github.com/Rin0107/ideas/internal/state"
)

func TestWorkersAndValid(t *testing.T) {
	if !slices.Equal(Workers(), []string{"sre", "designer", "engineer"}) {
		t.Fatal(Workers())
	}
	for r, want := range map[string]bool{"sre": true, "designer": true, "engineer": true, "leader": false, "": false, "SRE": false} {
		if Valid(r) != want {
			t.Errorf("Valid(%q) = %v", r, !want)
		}
	}
	if Label("sre") != "role:sre" {
		t.Fatal(Label("sre"))
	}
}

func TestForIssue(t *testing.T) {
	for _, c := range []struct {
		labels []string
		want   string
	}{
		{nil, Engineer},
		{[]string{"wk:task"}, Engineer},
		{[]string{"wk:task", "role:designer"}, Designer},
		{[]string{"role:sre"}, SRE},
		{[]string{"role:engineer"}, Engineer},
		{[]string{"role:leader"}, Engineer},          // leader は手を動かさない
		{[]string{"role:engineer", "role:sre"}, SRE}, // 2 つ付いていたら安全が先
		{[]string{"role:designer", "role:engineer"}, Designer},
		{[]string{"xrole:sre", "role:srex"}, Engineer}, // 完全に一致するものだけ
	} {
		if got := ForIssue(c.labels); got != c.want {
			t.Errorf("ForIssue(%v) = %s (want %s)", c.labels, got, c.want)
		}
	}
}

// 全部の段階で担当が決まる: ship だけ SRE、ほかは leader
func TestForPhase(t *testing.T) {
	phases := []string{state.PhaseIdle, state.PhaseRetro, state.PhaseBuild, state.PhaseEvaluate, state.PhaseIterate,
		state.PhaseShip, state.PhaseDrop, state.PhaseIntake, state.PhaseSelect, state.PhasePatrol, state.PhaseTriage, "?"}
	n := 0
	for _, p := range phases {
		want := Leader
		if p == "ship" {
			want = SRE
		}
		if got := ForPhase(p); got != want {
			t.Errorf("ForPhase(%s) = %s", p, got)
		}
		n++
	}
	if n != 12 {
		t.Fatal(n)
	}
}

func TestTouchesUIAndReviewers(t *testing.T) {
	for _, c := range []struct {
		files []string
		ui    bool
	}{
		{nil, false},
		{[]string{""}, false},
		{[]string{"internal/app/counter.go", "README.md", "spec/Counter.tla", "tests/unit/a.test.ts"}, false},
		{[]string{"src/worker/index.ts"}, false}, // 配信の Worker は画面でない
		{[]string{"src/worker/index.ts", "src/app.tsx"}, true},
		{[]string{"src/app.tsx"}, true},
		{[]string{" src/styles/tokens.css\n"}, true}, // 前後の空白は無視
		{[]string{"index.html"}, true},
		{[]string{"sub/index.html"}, false},
		{[]string{"public/icon.png"}, true},
		{[]string{"cmd/app/main.go"}, true}, // CLI の表示
		{[]string{"docs/design-system.md"}, true},
		{[]string{"docs/architecture.md"}, false},
		{[]string{"srcx/a.ts", "publicx/a", "cmdx/a"}, false}, // 接頭辞はディレクトリの区切りまで
	} {
		if got := TouchesUI(c.files); got != c.ui {
			t.Errorf("TouchesUI(%v) = %v", c.files, got)
		}
		want := []string{SRE, Engineer}
		if c.ui {
			want = []string{SRE, Engineer, Designer}
		}
		if got := Reviewers(c.files); !slices.Equal(got, want) {
			t.Errorf("Reviewers(%v) = %v", c.files, got)
		}
	}
}
