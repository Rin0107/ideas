package next

import (
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
