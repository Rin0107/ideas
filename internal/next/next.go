// Package next は「次の tick で何をするか」を決める。
// LLM ではなくここで決めることで、流れが毎回同じルールで進む。純粋関数なのでテストで固定する。
package next

import (
	"fmt"
	"slices"
	"time"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/state"
)

const LabelBlocked = "wk:blocked"

type Issue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Labels []string `json:"labels"`
}

func (i Issue) Blocked() bool { return slices.Contains(i.Labels, LabelBlocked) }

// Attempts は wk:attempt-N ラベルの最大の N。
func (i Issue) Attempts() int {
	n := 0
	for _, l := range i.Labels {
		var k int
		if _, err := fmt.Sscanf(l, "wk:attempt-%d", &k); err == nil && k > n {
			n = k
		}
	}
	return n
}

type Snapshot struct {
	Now     time.Time
	Cfg     *config.Config
	Active  *state.Project
	Issues  []Issue // Active の今のマイルストーンの open な issue
	Meta    *state.Meta
	Untried int
}

type Action struct {
	Phase  string `json:"phase"`
	Repo   string `json:"repo,omitempty"`
	Idea   string `json:"idea,omitempty"`
	Issue  int    `json:"issue,omitempty"`
	Reason string `json:"reason"`
}

func Decide(s Snapshot) Action {
	c := s.Cfg
	if state.DaysSince(s.Meta.LastRetro, s.Now) >= c.RetroEveryDays && s.Meta.LastRetro != "" ||
		s.Meta.LastRetro == "" && s.Active != nil && s.Active.Ticks.Total >= c.MaxTicksMVP {
		// 初回の retro は、最初のアプリがある程度進んでから
		return Action{Phase: state.PhaseRetro, Reason: "前回の retro から " + fmt.Sprint(c.RetroEveryDays) + " 日以上"}
	}

	if p := s.Active; p != nil {
		a := Action{Repo: p.Repo, Idea: p.Idea}
		switch {
		case p.Status == state.Selected:
			a.Phase, a.Reason = state.PhaseSelect, "選定の途中 (repo・issue の作成) から再開"
			return a
		case p.Verdict == "ship":
			a.Phase, a.Reason = state.PhaseShip, "評価が ship"
			return a
		case p.Verdict == "iterate":
			a.Phase, a.Reason = state.PhaseIterate, "評価が iterate"
			return a
		case p.Verdict == "drop":
			a.Phase, a.Reason = state.PhaseDrop, "評価が drop"
			return a
		case p.Status == state.Evaluating:
			a.Phase, a.Reason = state.PhaseEvaluate, "評価の途中"
			return a
		}
		limit := c.MaxTicksMVP
		if p.Status == state.Iterating {
			limit = c.MaxTicksIteration
		}
		if p.Ticks.Phase >= limit {
			a.Phase, a.Reason = state.PhaseEvaluate, fmt.Sprintf("時間切れ (%d/%d tick)", p.Ticks.Phase, limit)
			return a
		}
		for _, is := range sorted(s.Issues) {
			if !is.Blocked() {
				a.Phase, a.Issue = state.PhaseBuild, is.Number
				a.Reason = fmt.Sprintf("未完了の issue #%d (%d/%d tick)", is.Number, p.Ticks.Phase, limit)
				return a
			}
		}
		a.Phase = state.PhaseEvaluate
		if len(s.Issues) > 0 {
			a.Reason = "残りの issue が全部 blocked"
		} else {
			a.Reason = "マイルストーン " + p.Milestone + " の issue が全部閉じた"
		}
		return a
	}

	if s.Untried < c.MinUntriedIdeas {
		return Action{Phase: state.PhaseIntake, Reason: fmt.Sprintf("未着手のアイディアが %d 件 (< %d)", s.Untried, c.MinUntriedIdeas)}
	}
	if state.DaysSince(s.Meta.LastIntake, s.Now) >= c.IntakeEveryDays {
		return Action{Phase: state.PhaseIntake, Reason: fmt.Sprintf("前回の intake から %d 日以上", c.IntakeEveryDays)}
	}
	if s.Untried == 0 {
		return Action{Phase: state.PhaseIdle, Reason: "選べるアイディアがない"}
	}
	return Action{Phase: state.PhaseSelect, Reason: "作っているアプリがない"}
}

func sorted(is []Issue) []Issue {
	out := slices.Clone(is)
	slices.SortFunc(out, func(a, b Issue) int { return a.Number - b.Number })
	return out
}
