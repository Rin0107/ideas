package next

import (
	"slices"
	"testing"
	"time"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/state"
)

var now = time.Date(2026, 11, 10, 9, 0, 0, 0, time.Local)

func cfg() *config.Config {
	return &config.Config{
		MaxTicksMVP: 30, MaxTicksIteration: 15, RetroEveryDays: 7,
		IntakeEveryDays: 7, MinUntriedIdeas: 30,
	}
}

func meta() *state.Meta { return &state.Meta{LastIntake: "2026-11-09", LastRetro: "2026-11-09"} }

func building(phaseTicks int) *state.Project {
	return &state.Project{Idea: "00141-regex-crossword", Repo: "Rin0107/regex-crossword", Status: state.Building,
		Milestone: "MVP", Ticks: state.Ticks{Total: phaseTicks, Phase: phaseTicks}}
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name  string
		s     Snapshot
		phase string
		issue int
	}{
		{"アプリなし・アイディア十分 → select",
			Snapshot{Untried: 500}, state.PhaseSelect, 0},
		{"アプリなし・アイディア不足 → intake",
			Snapshot{Untried: 3}, state.PhaseIntake, 0},
		{"アプリなし・intake が古い → intake",
			Snapshot{Untried: 500, Meta: &state.Meta{LastIntake: "2026-11-01", LastRetro: "2026-11-09"}}, state.PhaseIntake, 0},
		{"retro が古い → retro を最優先",
			Snapshot{Untried: 500, Active: building(3), Issues: []Issue{{Number: 2}},
				Meta: &state.Meta{LastIntake: "2026-11-09", LastRetro: "2026-11-01"}}, state.PhaseRetro, 0},
		{"retro 未実施・最初のアプリが始まったばかり → build",
			Snapshot{Untried: 500, Active: building(3), Issues: []Issue{{Number: 2}},
				Meta: &state.Meta{LastIntake: "2026-11-09"}}, state.PhaseBuild, 2},
		{"build は番号の小さい blocked でない issue",
			Snapshot{Active: building(3), Issues: []Issue{{Number: 5}, {Number: 2, Labels: []string{LabelBlocked}}, {Number: 3}}},
			state.PhaseBuild, 3},
		{"直す issue (wk:fix) が番号より先",
			Snapshot{Active: building(3), Issues: []Issue{{Number: 2}, {Number: 8, Labels: []string{LabelFix}},
				{Number: 5, Labels: []string{LabelFix, LabelBlocked}}, {Number: 6, Labels: []string{LabelFix}}, {Number: 1}}},
			state.PhaseBuild, 6},
		{"工場の直す issue → 何より先に repair",
			Snapshot{Untried: 500, Active: building(3), Issues: []Issue{{Number: 2}}, Meta: &state.Meta{LastRetro: "2026-11-01"},
				FactoryFixes: []Issue{{Number: 9, Labels: []string{LabelFix, LabelBlocked}}, {Number: 7, Labels: []string{LabelFix}}}},
			state.PhaseRepair, 7},
		{"工場の直す issue が全部 blocked → いつもの段",
			Snapshot{Active: building(3), Issues: []Issue{{Number: 2}},
				FactoryFixes: []Issue{{Number: 9, Labels: []string{LabelFix, LabelBlocked}}}},
			state.PhaseBuild, 2},
		{"issue が全部閉じた → evaluate",
			Snapshot{Active: building(10)}, state.PhaseEvaluate, 0},
		{"残りが全部 blocked → evaluate",
			Snapshot{Active: building(10), Issues: []Issue{{Number: 4, Labels: []string{LabelBlocked}}}}, state.PhaseEvaluate, 0},
		{"時間切れ → evaluate",
			Snapshot{Active: building(30), Issues: []Issue{{Number: 4}}}, state.PhaseEvaluate, 0},
		{"iterating は短い上限で時間切れ",
			Snapshot{Active: func() *state.Project { p := building(15); p.Status = state.Iterating; return p }(),
				Issues: []Issue{{Number: 9}}}, state.PhaseEvaluate, 0},
		{"verdict ship → ship",
			Snapshot{Active: func() *state.Project { p := building(10); p.Status = state.Evaluating; p.Verdict = "ship"; return p }()},
			state.PhaseShip, 0},
		{"verdict iterate → iterate",
			Snapshot{Active: func() *state.Project { p := building(10); p.Status = state.Evaluating; p.Verdict = "iterate"; return p }()},
			state.PhaseIterate, 0},
		{"verdict drop → drop",
			Snapshot{Active: func() *state.Project { p := building(10); p.Status = state.Evaluating; p.Verdict = "drop"; return p }()},
			state.PhaseDrop, 0},
		// PBT (spec_test) だけに任せると、引かない回にカバレッジが欠ける (2026-11-10 に CI で起きた)
		{"評価の途中 (結論まだ) → evaluate を再開",
			Snapshot{Active: func() *state.Project { p := building(3); p.Status = state.Evaluating; return p }(),
				Issues: []Issue{{Number: 9}}},
			state.PhaseEvaluate, 0},
		{"selected のまま → select を再開",
			Snapshot{Active: func() *state.Project { p := building(0); p.Status = state.Selected; return p }()},
			state.PhaseSelect, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := c.s
			s.Now, s.Cfg = now, cfg()
			if s.Meta == nil {
				s.Meta = meta()
			}
			got := Decide(s)
			if got.Phase != c.phase || got.Issue != c.issue {
				t.Fatalf("got %s #%d (%s), want %s #%d", got.Phase, got.Issue, got.Reason, c.phase, c.issue)
			}
		})
	}
}

func TestAttempts(t *testing.T) {
	is := Issue{Labels: []string{"wk:task", "wk:attempt-1", "wk:attempt-2"}}
	if is.Attempts() != 2 {
		t.Fatal(is.Attempts())
	}
}

func TestRepairAction(t *testing.T) {
	s := Snapshot{Now: now, Cfg: cfg(), Meta: meta(), FactoryRepo: "o/factory",
		FactoryFixes: []Issue{{Number: 3, Labels: []string{LabelFix, "role:sre"}}}}
	a := Decide(s)
	if a.Repo != "o/factory" || !a.Fix || a.Role != "sre" || a.Reason != "工場の直す issue #3" {
		t.Fatalf("%+v", a)
	}
	s = Snapshot{Now: now, Cfg: cfg(), Meta: meta(), Active: building(3), Issues: []Issue{{Number: 4, Labels: []string{LabelFix}}, {Number: 1}}}
	if a := Decide(s); !a.Fix || a.Issue != 4 {
		t.Fatalf("%+v", a)
	}
	s.Issues = []Issue{{Number: 1}, {Number: 4, Labels: []string{LabelFix}}}
	if a := Decide(s); !a.Fix || a.Issue != 4 {
		t.Fatalf("並びに左右された %+v", a)
	}
	s.Issues = []Issue{{Number: 1}}
	if a := Decide(s); a.Fix {
		t.Fatalf("%+v", a)
	}
}

func TestBatch(t *testing.T) {
	is := []Issue{{Number: 4}, {Number: 2}, {Number: 3, Labels: []string{LabelBlocked}}, {Number: 9, Labels: []string{LabelFix}}}
	if got := batch(is, is[0]); !slices.Equal(got, []int{2, 4}) {
		t.Fatal(got)
	}
	if got := batch(is, is[3]); !slices.Equal(got, []int{9}) {
		t.Fatal(got)
	}
}

// v3.1: 作りかけを何件も持ち、ほかの実行が進めているもの (Claimed) は選ばない
func TestDecideParallel(t *testing.T) {
	a := building(3)
	b := &state.Project{Idea: "00200-b", Repo: "Rin0107/b", Status: state.Building, Milestone: "MVP"}
	sel := &state.Project{Idea: "00300-c", Status: state.Selected}
	apps := []App{{Project: a, Issues: []Issue{{Number: 2}}}, {Project: b, Issues: []Issue{{Number: 7}}, Proposals: 1}}
	base := func() Snapshot {
		c := cfg()
		c.MaxWIP = 3
		return Snapshot{Now: now, Cfg: c, Meta: meta(), Untried: 500, Apps: apps}
	}
	cases := []struct {
		name  string
		mut   func(*Snapshot)
		phase string
		repo  string
	}{
		{"いちばん古いものから", func(s *Snapshot) {}, state.PhaseBuild, a.Repo},
		{"前の段と同じアプリを続ける", func(s *Snapshot) { s.Prefer = b.Repo }, state.PhaseTriage, b.Repo},
		{"ほかの実行が進めているものは飛ばす", func(s *Snapshot) { s.Claimed = map[string]bool{a.Repo: true} }, state.PhaseTriage, b.Repo},
		{"続けたいものでも、ほかの実行が持っていれば飛ばす",
			func(s *Snapshot) { s.Prefer, s.Claimed = b.Repo, map[string]bool{b.Repo: true} }, state.PhaseBuild, a.Repo},
		{"全部が持たれていて WIP に空きがあれば、新しいものを選ぶ",
			func(s *Snapshot) { s.Claimed = map[string]bool{a.Repo: true, b.Repo: true} }, state.PhaseSelect, ""},
		{"repo のない選定中のものは idea で見分ける",
			func(s *Snapshot) {
				s.Apps = []App{{Project: sel}, apps[0]}
				s.Claimed = map[string]bool{sel.Idea: true, state.PhaseSelect: true}
			}, state.PhaseBuild, a.Repo},
		{"選定中のものを持つ実行がいなければ、選定を再開",
			func(s *Snapshot) { s.Apps = []App{{Project: sel}} }, state.PhaseSelect, ""},
		{"WIP がいっぱいで全部持たれていれば idle",
			func(s *Snapshot) { s.Cfg.MaxWIP = 2; s.Claimed = map[string]bool{a.Repo: true, b.Repo: true} }, state.PhaseIdle, ""},
		{"ほかの実行が選んでいる最中なら、選ばずに idle",
			func(s *Snapshot) { s.Claimed = map[string]bool{a.Repo: true, b.Repo: true, state.PhaseSelect: true} }, state.PhaseIdle, ""},
		{"ほかの実行がアイディアを足している最中も idle",
			func(s *Snapshot) { s.Claimed = map[string]bool{a.Repo: true, b.Repo: true, state.PhaseIntake: true} }, state.PhaseIdle, ""},
		{"retro はほかの実行がやっていれば飛ばす",
			func(s *Snapshot) {
				s.Meta = &state.Meta{LastIntake: "2026-11-09"}
				s.Claimed = map[string]bool{state.PhaseRetro: true}
			},
			state.PhaseBuild, a.Repo},
		{"初回の retro は、どれかのアプリが進んでから (2 つ目でも)",
			func(s *Snapshot) {
				s.Meta = &state.Meta{LastIntake: "2026-11-09"}
				s.Apps = []App{apps[0], {Project: &state.Project{Idea: "x", Repo: "R/x", Status: state.Building, Ticks: state.Ticks{Total: 30}}}}
			}, state.PhaseRetro, ""},
		{"主力のアプリがあれば、それだけを進める (古いものより先)",
			func(s *Snapshot) { s.Cfg.Focus = b.Idea }, state.PhaseTriage, b.Repo},
		{"主力のアプリをほかの実行が持っていれば、ほかを進めず idle",
			func(s *Snapshot) { s.Cfg.Focus, s.Claimed = b.Idea, map[string]bool{b.Repo: true} }, state.PhaseIdle, ""},
		{"主力のアプリがまだ作りかけになければ、選ばずに待つ",
			func(s *Snapshot) { s.Cfg.Focus = "00999-closet" }, state.PhaseIdle, ""},
		{"主力のアプリがあっても retro はする",
			func(s *Snapshot) {
				s.Cfg.Focus = b.Idea
				s.Meta = &state.Meta{LastIntake: "2026-11-09"}
				s.Apps = []App{apps[0], {Project: &state.Project{Idea: "x", Repo: "R/x", Status: state.Building, Ticks: state.Ticks{Total: 30}}}}
			}, state.PhaseRetro, ""},
		{"工場の直す issue もほかの実行がやっていれば飛ばす",
			func(s *Snapshot) {
				s.FactoryFixes = []Issue{{Number: 9, Labels: []string{LabelFix}}}
				s.Claimed = map[string]bool{state.PhaseRepair: true}
			}, state.PhaseBuild, a.Repo},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := base()
			c.mut(&s)
			got := Decide(s)
			if got.Phase != c.phase || got.Repo != c.repo {
				t.Fatalf("got %s %s (%s), want %s %s", got.Phase, got.Repo, got.Reason, c.phase, c.repo)
			}
		})
	}
	s := base()
	s.Cfg.MaxWIP = 0 // 0 は 1
	s.Apps, s.Claimed = apps[:1], map[string]bool{a.Repo: true}
	if got := Decide(s); got.Phase != state.PhaseIdle || got.Reason != "作りかけが 1 件 (上限 1)。どれもほかの実行が進めている" {
		t.Fatalf("%+v", got)
	}
	s.Apps, s.Cfg.MaxWIP = nil, 2
	if got := Decide(s); got.Reason != "作りかけが 0 件 (上限 2)" {
		t.Fatalf("%+v", got)
	}
	if !Global[state.PhaseSelect] || Global[state.PhaseBuild] {
		t.Fatal("全体で 1 つの段")
	}
}
