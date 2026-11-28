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

// 2026-11-28: 前の計画の task が 1 件も終わっていなければ評価しない (夜に PC が止まると、雛形のまま採点して drop しかねない)
func TestDecidePlannerNoProgress(t *testing.T) {
	task := func(n int, plan string) *state.Task { return &state.Task{Issue: n, Plan: plan} }
	open := []Issue{{Number: 5}, {Number: 6}}
	cases := []struct {
		name   string
		status string
		tasks  *state.Tasks
		issues []Issue
		phase  string
	}{
		{"task が全部開いている", state.Iterating, tasksFor(task(5, "2026-11-09"), task(6, "2026-11-09")), open, state.PhaseIdle},
		{"1 件閉じた", state.Iterating, tasksFor(task(5, "2026-11-09"), task(7, "2026-11-09")), open, state.PhaseEvaluate},
		{"閉じたのは前の計画の task だけ", state.Iterating, tasksFor(task(3, "2026-11-08"), task(5, "2026-11-09"), task(6, "2026-11-09")), open, state.PhaseIdle},
		{"最新の計画が先に並んでも同じ", state.Iterating, tasksFor(task(6, "2026-11-09"), task(3, "2026-11-08"), task(5, "2026-11-09")), open, state.PhaseIdle},
		{"task の記録が空", state.Iterating, &state.Tasks{Tasks: map[string]*state.Task{}}, open, state.PhaseEvaluate},
		{"評価の途中は続ける", state.Evaluating, tasksFor(task(5, "2026-11-09")), open, state.PhaseEvaluate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := building(40)
			p.Status, p.Iteration, p.Planned = c.status, 1, "2026-11-08"
			a := Decide(Snapshot{Now: now, Cfg: v4cfg(), Meta: meta(), Planner: true, Apps: []App{{Project: p, Issues: c.issues, Tasks: c.tasks}}})
			if a.Phase != c.phase || a.Reason == "" {
				t.Fatalf("%+v", a)
			}
			if c.phase == state.PhaseIdle && a.Reason != "前の計画の task がまだ 1 件も終わっていない (評価しても前と同じ)" {
				t.Fatalf("%+v", a)
			}
		})
	}
}

// 主力の 1 つ目に進める段がなければ、2 つ目を見る (2026-11-28: 夢日記SNS が待ちで、夜ふかし星座の最初の計画に進めなかった)
func TestDecideFocusSkipsIdle(t *testing.T) {
	waiting := func(repo, idea string) App {
		p := building(40)
		p.Repo, p.Idea, p.Status, p.Iteration, p.Planned = repo, idea, state.Iterating, 1, state.PlanNight(now)
		return App{Project: p}
	}
	fresh := building(0)
	fresh.Repo, fresh.Idea = "Rin0107/b", "00002-b"
	c := v4cfg()
	c.Focus, c.MaxWIP = config.Focus{"00001-a", "00002-b"}, 4
	s := Snapshot{Now: now, Cfg: c, Meta: meta(), Planner: true, Untried: 500, Apps: []App{waiting("Rin0107/a", "00001-a"), {Project: fresh}}}
	if a := Decide(s); a.Phase != state.PhasePlan || a.Repo != "Rin0107/b" {
		t.Fatalf("2 つ目の計画に進まない: %+v", a)
	}
	// どれも待ちで、始めていない主力もなければ、最初の待ちの理由を返す
	s.Apps = []App{waiting("Rin0107/a", "00001-a"), waiting("Rin0107/b", "00002-b")}
	if a := Decide(s); a.Phase != state.PhaseIdle || a.Repo != "Rin0107/a" || a.Reason != "今夜の計画は作った" {
		t.Fatalf("%+v", a)
	}
}
