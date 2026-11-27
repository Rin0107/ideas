package next

import (
	"testing"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/state"
)

func v4cfg() *config.Config { c := cfg(); c.V4.Enabled = true; return c }

func tasksFor(issues ...*state.Task) *state.Tasks {
	t := &state.Tasks{Tasks: map[string]*state.Task{}}
	for _, is := range issues {
		t.Put(is)
	}
	return t
}

func TestDecideTask(t *testing.T) {
	iter := func(mut ...func(*state.Project)) *state.Project {
		p := building(0)
		p.Status = state.Iterating
		for _, m := range mut {
			m(p)
		}
		return p
	}
	tasks := tasksFor(&state.Task{Issue: 3}, &state.Task{Issue: 4, After: []int{3}}, &state.Task{Issue: 5})
	cases := []struct {
		name     string
		s        Snapshot
		phase    string
		issue    int
		wantRole string
	}{
		{"番号の小さい task から", Snapshot{Apps: []App{{Project: iter(), Issues: []Issue{{Number: 4}, {Number: 3}}, Tasks: tasks}}}, state.PhaseTask, 3, "engineer"},
		{"先の task が open なら待つ", Snapshot{Apps: []App{{Project: iter(), Issues: []Issue{{Number: 3, Labels: []string{LabelBlocked}}, {Number: 4}}, Tasks: tasks}}}, state.PhaseIdle, 0, ""},
		{"先の task が閉じていれば進む", Snapshot{Apps: []App{{Project: iter(), Issues: []Issue{{Number: 4, Labels: []string{"role:designer"}}}, Tasks: tasks}}}, state.PhaseTask, 4, "designer"},
		{"ブロックは飛ばす", Snapshot{Apps: []App{{Project: iter(), Issues: []Issue{{Number: 3, Labels: []string{LabelBlocked}}, {Number: 5}}, Tasks: tasks}}}, state.PhaseTask, 5, "engineer"},
		{"計画の task でない issue はやらない", Snapshot{Apps: []App{{Project: iter(), Issues: []Issue{{Number: 9}}, Tasks: tasks}}}, state.PhaseIdle, 0, ""},
		{"task がない", Snapshot{Apps: []App{{Project: iter(), Issues: []Issue{{Number: 3}}}}}, state.PhaseIdle, 0, ""},
		{"評価が出ている (計画待ち)", Snapshot{Apps: []App{{Project: iter(func(p *state.Project) { p.Verdict = "iterate" }), Issues: []Issue{{Number: 3}}, Tasks: tasks}}}, state.PhaseIdle, 0, ""},
		{"ほかの実行が進めている", Snapshot{Claimed: map[string]bool{"Rin0107/regex-crossword": true}, Apps: []App{{Project: iter(), Issues: []Issue{{Number: 3}}, Tasks: tasks}}}, state.PhaseIdle, 0, ""},
		{"工場の直す issue があってもやらない (計画がやる)", Snapshot{FactoryFixes: []Issue{{Number: 1}}, Apps: []App{{Project: iter(), Issues: []Issue{{Number: 3}}, Tasks: tasks}}}, state.PhaseTask, 3, "engineer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.s.Now, c.s.Cfg, c.s.Meta = now, v4cfg(), meta()
			a := Decide(c.s)
			if a.Phase != c.phase || a.Issue != c.issue || c.wantRole != "" && a.Role != c.wantRole {
				t.Fatalf("%+v", a)
			}
			if a.Phase == state.PhaseTask && (a.Repo != "Rin0107/regex-crossword" || len(a.Issues) != 1 || a.Issues[0] != c.issue || a.Idea == "") {
				t.Fatalf("%+v", a)
			}
		})
	}
	// 主力のアプリがあれば、それだけ
	c := v4cfg()
	c.Focus = config.Focus{"00999-other"}
	a := Decide(Snapshot{Now: now, Cfg: c, Meta: meta(), Apps: []App{{Project: iter(), Issues: []Issue{{Number: 3}}, Tasks: tasks}}})
	if a.Phase != state.PhaseIdle {
		t.Fatalf("%+v", a)
	}
	c.Focus = config.Focus{"00141-regex-crossword"}
	if a := Decide(Snapshot{Now: now, Cfg: c, Meta: meta(), Apps: []App{{Project: iter(), Issues: []Issue{{Number: 3}}, Tasks: tasks}}}); a.Phase != state.PhaseTask {
		t.Fatalf("%+v", a)
	}
}

func TestDecidePlanner(t *testing.T) {
	today := state.PlanNight(now)
	cases := []struct {
		name  string
		mut   func(*state.Project)
		phase string
	}{
		{"選定の途中", func(p *state.Project) { p.Status = state.Selected }, state.PhaseSelect},
		{"ship", func(p *state.Project) { p.Verdict = "ship" }, state.PhaseShip},
		{"drop", func(p *state.Project) { p.Verdict = "drop" }, state.PhaseDrop},
		{"iterate なら計画", func(p *state.Project) { p.Verdict = "iterate"; p.Planned = today }, state.PhasePlan},
		{"今日はもう計画した", func(p *state.Project) { p.Status = state.Iterating; p.Planned = today }, state.PhaseIdle},
		{"最初の版は評価せずに計画", func(p *state.Project) {}, state.PhasePlan},
		{"最初の版でも計画したあとは評価", func(p *state.Project) { p.Planned = "2026-11-08" }, state.PhaseEvaluate},
		{"磨いている途中は評価から", func(p *state.Project) { p.Status = state.Iterating; p.Iteration = 1 }, state.PhaseEvaluate},
		{"building でも 2 回目以降は評価から", func(p *state.Project) { p.Iteration = 1 }, state.PhaseEvaluate},
		{"評価の途中", func(p *state.Project) { p.Status = state.Evaluating; p.Iteration = 1 }, state.PhaseEvaluate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := building(40) // tick の時間切れは見ない
			c.mut(p)
			a := Decide(Snapshot{Now: now, Cfg: v4cfg(), Meta: meta(), Planner: true, Apps: []App{{Project: p, Issues: []Issue{{Number: 3}}}}})
			if a.Phase != c.phase || a.Phase != state.PhaseIdle && a.Repo != p.Repo || a.Reason == "" {
				t.Fatalf("%+v", a)
			}
		})
	}
	// 工場の直す issue は計画の実行が先に直す
	a := Decide(Snapshot{Now: now, Cfg: v4cfg(), Meta: meta(), Planner: true, FactoryFixes: []Issue{{Number: 1}}, Apps: []App{{Project: building(0)}}})
	if a.Phase != state.PhaseRepair {
		t.Fatalf("%+v", a)
	}
}
