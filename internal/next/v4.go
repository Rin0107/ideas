package next

import (
	"fmt"
	"slices"
	"time"

	"github.com/Rin0107/ideas/internal/role"
	"github.com/Rin0107/ideas/internal/state"
)

// decideTask は tick の実行 (v4) の次の段: 計画の task のうち、先の task が終わっていて、ブロックされていないものを 1 件。
// 評価・計画・修理は計画の実行 (毎日 1 回) がやる。ここでは考えない
func decideTask(s Snapshot) Action {
	for _, ap := range s.apps() {
		p := ap.Project
		if s.Cfg.Focus != "" && p.Idea != s.Cfg.Focus || s.claimed(p) || ap.Tasks == nil || p.Verdict != "" {
			continue
		}
		open := map[int]bool{}
		for _, is := range ap.Issues {
			open[is.Number] = true
		}
		for _, is := range sorted(ap.Issues) {
			t := ap.Tasks.Get(is.Number)
			if t == nil || is.Blocked() || slices.ContainsFunc(t.After, func(n int) bool { return open[n] }) {
				continue
			}
			return Action{Phase: state.PhaseTask, Repo: p.Repo, Idea: p.Idea, Issue: is.Number, Issues: []int{is.Number}, Role: role.ForIssue(is.Labels),
				Reason: fmt.Sprintf("計画の task #%d", is.Number)}
		}
	}
	return Action{Phase: state.PhaseIdle, Reason: "できる task がない (残りはブロックか、先の task 待ち。次の計画を待つ)"}
}

// decidePlan は計画の実行 (v4) が作りかけのアプリ 1 つで次にやる段: 評価 → 計画。同じ日には 1 回だけ
func decidePlan(s Snapshot, ap App, a Action) Action {
	p := ap.Project
	if p.Status == state.Selected {
		a.Phase, a.Reason = state.PhaseSelect, "選定の途中 (repo・issue の作成) から再開"
		return a
	}
	if p.Verdict == "ship" || p.Verdict == "drop" {
		a.Phase, a.Reason = p.Verdict, "評価が "+p.Verdict
		return a
	}
	if p.Verdict == "iterate" {
		a.Phase, a.Reason = state.PhasePlan, "評価が iterate"
		return a
	}
	if p.Planned == s.Now.Format(time.DateOnly) {
		a.Phase, a.Reason = state.PhaseIdle, "今日の計画は作った"
		return a
	}
	if p.Status == state.Building && p.Iteration == 0 && p.Planned == "" {
		a.Phase, a.Reason = state.PhasePlan, "最初の版の計画"
		return a
	}
	a.Phase, a.Reason = state.PhaseEvaluate, "前の計画のあとを評価する"
	return a
}
