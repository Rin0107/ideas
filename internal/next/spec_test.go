package next

// spec/Factory.tla の不変条件を、実装 (Decide) に対して PBT で確かめる。
// TLA+ は「流れのモデル」が正しいことを、ここは「コードがそのモデルどおりに決める」ことを固定する。

import (
	"fmt"
	"slices"
	"testing"

	"github.com/Rin0107/ideas/internal/role"
	"github.com/Rin0107/ideas/internal/state"
	"pgregory.net/rapid"
)

var phases = []string{state.PhaseIdle, state.PhaseRetro, state.PhaseBuild, state.PhaseEvaluate, state.PhaseIterate,
	state.PhaseShip, state.PhaseDrop, state.PhaseIntake, state.PhaseSelect, state.PhasePatrol, state.PhaseTriage, state.PhaseRepair,
	state.PhasePlan, state.PhaseTask}

var roles = []string{role.Leader, role.SRE, role.Designer, role.Engineer}

// genSnapshot はでたらめな状態を作る (ありえない組み合わせも含めて、Decide が決まった形で答えるか)。
func genSnapshot(t *rapid.T) Snapshot {
	c := cfg()
	c.MaxTicksMVP = rapid.IntRange(1, 40).Draw(t, "mvp")
	c.MaxTicksIteration = rapid.IntRange(1, 20).Draw(t, "iter")
	c.MinUntriedIdeas = rapid.IntRange(0, 40).Draw(t, "minUntried")
	c.PatrolEveryDays = rapid.IntRange(-1, 5).Draw(t, "patrolEvery")
	c.PatrolMinTicks = rapid.IntRange(0, 10).Draw(t, "patrolMin")
	m := &state.Meta{
		LastIntake: rapid.SampledFrom([]string{"", "2026-11-01", "2026-11-09"}).Draw(t, "intake"),
		LastRetro:  rapid.SampledFrom([]string{"", "2026-11-01", "2026-11-09"}).Draw(t, "retro"),
		LastPatrol: map[string]string{},
	}
	for _, r := range role.Workers() {
		if d := rapid.SampledFrom([]string{"", "2026-11-01", "2026-11-08", "2026-11-10"}).Draw(t, "patrol-"+r); d != "" {
			m.LastPatrol[r] = d
		}
	}
	s := Snapshot{Now: now, Cfg: c, Meta: m, Untried: rapid.IntRange(0, 60).Draw(t, "untried")}
	if rapid.Bool().Draw(t, "active") {
		p := &state.Project{Idea: "00001-a", Repo: "o/a", Milestone: "MVP",
			Status:  rapid.SampledFrom([]string{state.Selected, state.Building, state.Evaluating, state.Iterating}).Draw(t, "status"),
			Verdict: rapid.SampledFrom([]string{"", "ship", "iterate", "drop"}).Draw(t, "verdict"),
		}
		p.Ticks.Phase = rapid.IntRange(0, 45).Draw(t, "phaseTicks")
		p.Ticks.Total = p.Ticks.Phase + rapid.IntRange(0, 30).Draw(t, "earlier")
		s.Active = p
		s.Proposals = rapid.IntRange(0, 3).Draw(t, "proposals")
		n := rapid.IntRange(0, 8).Draw(t, "issues")
		for i := 0; i < n; i++ {
			is := Issue{Number: rapid.IntRange(1, 50).Draw(t, fmt.Sprint("num", i))}
			if rapid.Bool().Draw(t, fmt.Sprint("blocked", i)) {
				is.Labels = []string{LabelBlocked}
			}
			if rapid.Bool().Draw(t, fmt.Sprint("fix", i)) {
				is.Labels = append(is.Labels, LabelFix)
			}
			if r := rapid.SampledFrom([]string{"", "sre", "designer", "engineer"}).Draw(t, fmt.Sprint("role", i)); r != "" {
				is.Labels = append(is.Labels, role.Label(r))
			}
			s.Issues = append(s.Issues, is)
		}
	}
	n := rapid.IntRange(0, 3).Draw(t, "factoryFixes")
	s.FactoryRepo = "o/factory"
	for i := 0; i < n; i++ {
		is := Issue{Number: rapid.IntRange(1, 20).Draw(t, fmt.Sprint("fnum", i)), Labels: []string{LabelFix}}
		if rapid.Bool().Draw(t, fmt.Sprint("fblocked", i)) {
			is.Labels = append(is.Labels, LabelBlocked)
		}
		s.FactoryFixes = append(s.FactoryFixes, is)
	}
	return s
}

// TypeOK: Decide はいつも決まった段階と担当のロールを返し、build なら blocked でない一番小さい issue を、その issue の担当に渡す。
func TestSpec_TypeOK(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSnapshot(t)
		a := Decide(s)
		if !slices.Contains(phases, a.Phase) || !slices.Contains(roles, a.Role) || a.Reason == "" {
			t.Fatalf("知らない段階 %+v", a)
		}
		if a.Phase != state.PhaseBuild {
			return
		}
		// 直す issue (wk:fix) があればその中から、なければ全部から
		fixFirst := slices.ContainsFunc(s.Issues, func(is Issue) bool { return !is.Blocked() && is.Fix() })
		pick := func(is Issue) bool { return !is.Blocked() && is.Fix() == fixFirst }
		lowest, owner := 0, ""
		for _, is := range s.Issues {
			if pick(is) && (lowest == 0 || is.Number < lowest) {
				lowest, owner = is.Number, role.ForIssue(is.Labels)
			}
		}
		if a.Issue != lowest || a.Fix != fixFirst {
			t.Fatalf("issue #%d (fix %v) を選んだ (選ぶべきは #%d fix %v)", a.Issue, a.Fix, lowest, fixFirst)
		}
		// 同じ番号の issue が 2 つ引かれることがあるので、担当は「その番号のどれかの担当」であればよい
		ok := a.Role == owner
		for _, is := range s.Issues {
			ok = ok || is.Number == lowest && pick(is) && role.ForIssue(is.Labels) == a.Role
		}
		if !ok {
			t.Fatalf("issue #%d の担当が %s", a.Issue, a.Role)
		}
	})
}

// VerdictOnlyWhenEvaluated: 結論を持っているあいだは、作り続けない (build・evaluate に戻らず、結論の段階に進む)。
func TestSpec_VerdictOnlyWhenEvaluated(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSnapshot(t)
		if s.Active == nil || s.Active.Verdict == "" || s.Active.Status == state.Selected {
			t.Skip()
		}
		a := Decide(s)
		if a.Phase != state.PhaseRetro && a.Phase != state.PhaseRepair && a.Phase != s.Active.Verdict {
			t.Fatalf("結論 %s なのに %s", s.Active.Verdict, a.Phase)
		}
	})
}

// NoBuildPastLimit: その段階の tick の上限に達したら、tick を使う仕事 (build・patrol・triage) をしない。
func TestSpec_NoBuildPastLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSnapshot(t)
		if s.Active == nil {
			t.Skip()
		}
		limit := s.Cfg.MaxTicksMVP
		if s.Active.Status == state.Iterating {
			limit = s.Cfg.MaxTicksIteration
		}
		a := Decide(s)
		working := a.Phase == state.PhaseBuild || a.Phase == state.PhasePatrol || a.Phase == state.PhaseTriage
		if working && s.Active.Ticks.Phase >= limit {
			t.Fatalf("上限 %d に達しているのに %s (%d tick)", limit, a.Phase, s.Active.Ticks.Phase)
		}
	})
}

func TestIdleWhenNothingToChoose(t *testing.T) {
	c := cfg()
	c.MinUntriedIdeas = 0
	if a := Decide(Snapshot{Now: now, Cfg: c, Meta: meta(), Untried: 0}); a.Phase != state.PhaseIdle {
		t.Fatal(a)
	}
}

// TriageFirst (spec の PROPERTY): 仕分けていない提案があるあいだは、build も patrol もしない。
// また、patrol と triage は作っている途中 (building / iterating) にだけ起きる。
func TestSpec_TriageFirst(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSnapshot(t)
		a := Decide(s)
		if s.Proposals > 0 && (a.Phase == state.PhaseBuild || a.Phase == state.PhasePatrol) {
			t.Fatalf("提案が %d 件あるのに %s", s.Proposals, a.Phase)
		}
		if a.Phase != state.PhasePatrol && a.Phase != state.PhaseTriage {
			return
		}
		if s.Active == nil || s.Active.Status != state.Building && s.Active.Status != state.Iterating || s.Active.Verdict != "" {
			t.Fatalf("作っている途中でないのに %s", a.Phase)
		}
		if a.Phase == state.PhasePatrol && !role.Valid(a.Role) {
			t.Fatalf("見回りの担当が %s", a.Role)
		}
	})
}

// RepairFirst: 工場の直す issue (wk:fix) が blocked でなく残っているあいだは、何より先にそれを直す (番号の小さい順)。
// なければ repair にはならない。
func TestSpec_RepairFirst(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSnapshot(t)
		a := Decide(s)
		lowest := 0
		for _, is := range s.FactoryFixes {
			if !is.Blocked() && (lowest == 0 || is.Number < lowest) {
				lowest = is.Number
			}
		}
		if lowest == 0 {
			if a.Phase == state.PhaseRepair {
				t.Fatalf("直す issue がないのに repair %+v", a)
			}
			return
		}
		if a.Phase != state.PhaseRepair || a.Issue != lowest || a.Repo != s.FactoryRepo || !a.Fix || !role.Valid(a.Role) {
			t.Fatalf("工場の直す issue #%d があるのに %+v", lowest, a)
		}
	})
}
