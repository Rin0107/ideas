package next

// spec/Factory.tla の不変条件を、実装 (Decide) に対して PBT で確かめる。
// TLA+ は「流れのモデル」が正しいことを、ここは「コードがそのモデルどおりに決める」ことを固定する。

import (
	"fmt"
	"slices"
	"testing"

	"github.com/Rin0107/ideas/internal/state"
	"pgregory.net/rapid"
)

var phases = []string{state.PhaseIdle, state.PhaseRetro, state.PhaseBuild, state.PhaseEvaluate, state.PhaseIterate,
	state.PhaseShip, state.PhaseDrop, state.PhaseIntake, state.PhaseSelect}

// genSnapshot はでたらめな状態を作る (ありえない組み合わせも含めて、Decide が決まった形で答えるか)。
func genSnapshot(t *rapid.T) Snapshot {
	c := cfg()
	c.MaxTicksMVP = rapid.IntRange(1, 40).Draw(t, "mvp")
	c.MaxTicksIteration = rapid.IntRange(1, 20).Draw(t, "iter")
	c.MinUntriedIdeas = rapid.IntRange(0, 40).Draw(t, "minUntried")
	m := &state.Meta{
		LastIntake: rapid.SampledFrom([]string{"", "2026-11-01", "2026-11-09"}).Draw(t, "intake"),
		LastRetro:  rapid.SampledFrom([]string{"", "2026-11-01", "2026-11-09"}).Draw(t, "retro"),
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
		n := rapid.IntRange(0, 8).Draw(t, "issues")
		for i := 0; i < n; i++ {
			is := Issue{Number: rapid.IntRange(1, 50).Draw(t, fmt.Sprint("num", i))}
			if rapid.Bool().Draw(t, fmt.Sprint("blocked", i)) {
				is.Labels = []string{LabelBlocked}
			}
			s.Issues = append(s.Issues, is)
		}
	}
	return s
}

// TypeOK: Decide はいつも決まった段階のどれかを返し、build なら blocked でない一番小さい issue を選ぶ。
func TestSpec_TypeOK(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSnapshot(t)
		a := Decide(s)
		if !slices.Contains(phases, a.Phase) || a.Reason == "" {
			t.Fatalf("知らない段階 %+v", a)
		}
		if a.Phase != state.PhaseBuild {
			return
		}
		lowest := 0
		for _, is := range s.Issues {
			if !is.Blocked() && (lowest == 0 || is.Number < lowest) {
				lowest = is.Number
			}
		}
		if a.Issue != lowest {
			t.Fatalf("issue #%d を選んだ (一番小さい未 blocked は #%d)", a.Issue, lowest)
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
		if a.Phase != state.PhaseRetro && a.Phase != s.Active.Verdict {
			t.Fatalf("結論 %s なのに %s", s.Active.Verdict, a.Phase)
		}
	})
}

// NoBuildPastLimit: その段階の tick の上限に達したら、build しない。
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
		if a := Decide(s); a.Phase == state.PhaseBuild && s.Active.Ticks.Phase >= limit {
			t.Fatalf("上限 %d に達しているのに build (%d tick)", limit, s.Active.Ticks.Phase)
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
