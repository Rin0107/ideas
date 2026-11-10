// Package next は「次の tick で何をするか」を決める。
// LLM ではなくここで決めることで、流れが毎回同じルールで進む。純粋関数なのでテストで固定する。
package next

import (
	"fmt"
	"slices"
	"time"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/role"
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
	// Proposals は Active の repo にある、まだ仕分けていない提案 (wk:proposal) の数
	Proposals int
}

type Action struct {
	Phase  string `json:"phase"`
	Repo   string `json:"repo,omitempty"`
	Idea   string `json:"idea,omitempty"`
	Issue  int    `json:"issue,omitempty"`
	Role   string `json:"role"` // 担当のロール (docs/ROLES.md)
	Reason string `json:"reason"`
}

// Decide は次の段階と、その担当のロールを決める。
func Decide(s Snapshot) Action {
	a := decide(s)
	if a.Role == "" {
		a.Role = role.ForPhase(a.Phase)
	}
	return a
}

func decide(s Snapshot) Action {
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
		// ロールのループ: 提案があれば先に仕分ける。なければ、見回りの番のロールが見回る。
		// どちらもこの段階の tick に数える (wk next)。だから時間切れは必ず来て、アプリは必ず評価に進む
		if s.Proposals > 0 {
			a.Phase, a.Reason = state.PhaseTriage, fmt.Sprintf("仕分けていない提案が %d 件", s.Proposals)
			return a
		}
		if r := patrolDue(s); r != "" {
			a.Phase, a.Role, a.Reason = state.PhasePatrol, r, fmt.Sprintf("%s の見回りから %d 日以上", r, c.PatrolEveryDays)
			return a
		}
		for _, is := range sorted(s.Issues) {
			if !is.Blocked() {
				a.Phase, a.Issue, a.Role = state.PhaseBuild, is.Number, role.ForIssue(is.Labels)
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

// patrolDue は見回りの番のロールを返す (なければ空)。
// 見回るのは、アプリがある程度進んでから (見るものがない)。順番は role.Workers (安全が先)。
func patrolDue(s Snapshot) string {
	c := s.Cfg
	if c.PatrolEveryDays <= 0 || s.Active.Ticks.Total < c.PatrolMinTicks {
		return ""
	}
	for _, r := range role.Workers() {
		if state.DaysSince(s.Meta.LastPatrol[r], s.Now) >= c.PatrolEveryDays {
			return r
		}
	}
	return ""
}

func sorted(is []Issue) []Issue {
	out := slices.Clone(is)
	slices.SortFunc(out, func(a, b Issue) int { return a.Number - b.Number })
	return out
}
