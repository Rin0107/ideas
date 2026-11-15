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

const (
	LabelBlocked = "wk:blocked"
	LabelFix     = "wk:fix" // 工場が自分で直す問題 (tick の結果 fix から立つ)。ほかの issue より先に解く
)

type Issue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Labels []string `json:"labels"`
}

func (i Issue) Blocked() bool { return slices.Contains(i.Labels, LabelBlocked) }
func (i Issue) Fix() bool     { return slices.Contains(i.Labels, LabelFix) }

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
	// FactoryFixes は工場 repo の open な直す issue (wk:fix)
	FactoryFixes []Issue
	FactoryRepo  string

	// v3.1: 同時に何本も動く (docs/DESIGN-v3.md §8)
	// Apps は作りかけのアプリ全部 (古い順)。空なら Active・Issues・Proposals の 1 つだけ
	Apps []App
	// Claimed はほかの実行が進めているもの (repo・idea・全体の段の名前)。これは選ばない
	Claimed map[string]bool
	// Prefer はこの実行が前の段で進めた repo。続けて同じアプリを進める (読んだものを使い回せる)
	Prefer string
}

// App は作りかけのアプリ 1 つと、その今の issue・提案。
type App struct {
	Project   *state.Project
	Issues    []Issue
	Proposals int
}

// Global は全体で 1 つずつしか動かさない段 (2 つの実行が同時にやると、同じものを 2 回作る)。
var Global = map[string]bool{state.PhaseRepair: true, state.PhaseRetro: true, state.PhaseIntake: true, state.PhaseSelect: true}

// apps は進める候補を、続けたいアプリ (Prefer) を先にして返す。
func (s Snapshot) apps() []App {
	out := slices.Clone(s.Apps)
	if len(out) == 0 && s.Active != nil {
		out = []App{{s.Active, s.Issues, s.Proposals}}
	}
	slices.SortStableFunc(out, func(a, b App) int {
		if a.Project.Repo == s.Prefer && b.Project.Repo != s.Prefer {
			return -1
		}
		if b.Project.Repo == s.Prefer && a.Project.Repo != s.Prefer {
			return 1
		}
		return 0
	})
	return out
}

// claimed は p をほかの実行が進めているか。
func (s Snapshot) claimed(p *state.Project) bool {
	return p.Repo != "" && s.Claimed[p.Repo] || s.Claimed[p.Idea]
}

type Action struct {
	Phase string `json:"phase"`
	Repo  string `json:"repo,omitempty"`
	Idea  string `json:"idea,omitempty"`
	Issue int    `json:"issue,omitempty"`
	Fix   bool   `json:"fix,omitempty"` // Issue が直す issue (wk:fix) か
	// Issues は build で一度に作る issue (v3: 1 回の実行で 1 版を作り切る。docs/DESIGN-v3.md §3)。Issue はその先頭
	Issues []int  `json:"issues,omitempty"`
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
	// 工場そのものが壊れていたら、何より先に直す (直さないと、ほかの段も同じところで詰まる)
	for _, is := range sorted(s.FactoryFixes) {
		if !is.Blocked() && !s.Claimed[state.PhaseRepair] {
			return Action{Phase: state.PhaseRepair, Repo: s.FactoryRepo, Issue: is.Number, Fix: true, Role: role.ForIssue(is.Labels),
				Reason: fmt.Sprintf("工場の直す issue #%d", is.Number)}
		}
	}
	apps := s.apps()
	started := false // 初回の retro は、どれかのアプリがある程度進んでから
	for _, ap := range apps {
		started = started || ap.Project.Ticks.Total >= c.MaxTicksMVP
	}
	if !s.Claimed[state.PhaseRetro] && (state.DaysSince(s.Meta.LastRetro, s.Now) >= c.RetroEveryDays && s.Meta.LastRetro != "" ||
		s.Meta.LastRetro == "" && started) {
		return Action{Phase: state.PhaseRetro, Reason: "前回の retro から " + fmt.Sprint(c.RetroEveryDays) + " 日以上"}
	}
	// 主力のアプリ (focus) があれば、それだけを進める。ほかの作りかけは止め、新しいアイディアは選ばない (docs/DESIGN-v3.md §10)
	if c.Focus != "" {
		return decideFocus(s, apps)
	}
	// 作りかけのアプリを、ほかの実行が進めていないものから 1 つ進める
	for _, ap := range apps {
		if !s.claimed(ap.Project) {
			return decideApp(s, ap)
		}
	}
	if len(apps) >= c.WIP() {
		return Action{Phase: state.PhaseIdle, Reason: fmt.Sprintf("作りかけが %d 件 (上限 %d)。どれもほかの実行が進めている", len(apps), c.WIP())}
	}
	if s.Claimed[state.PhaseSelect] || s.Claimed[state.PhaseIntake] {
		return Action{Phase: state.PhaseIdle, Reason: "ほかの実行が次のアイディアを選んでいる"}
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
	return Action{Phase: state.PhaseSelect, Reason: fmt.Sprintf("作りかけが %d 件 (上限 %d)", len(apps), c.WIP())}
}

// decideApp は作りかけのアプリ 1 つの次の段を決める。
func decideApp(s Snapshot, ap App) Action {
	c, p := s.Cfg, ap.Project
	a := Action{Repo: p.Repo, Idea: p.Idea}
	// switch の case の条件はミューテーションの道具がカバレッジの外と数えるので、if で書く
	if p.Status == state.Selected {
		a.Phase, a.Reason = state.PhaseSelect, "選定の途中 (repo・issue の作成) から再開"
		return a
	}
	if p.Verdict == "ship" {
		a.Phase, a.Reason = state.PhaseShip, "評価が ship"
		return a
	}
	if p.Verdict == "iterate" {
		a.Phase, a.Reason = state.PhaseIterate, "評価が iterate"
		return a
	}
	if p.Verdict == "drop" {
		a.Phase, a.Reason = state.PhaseDrop, "評価が drop"
		return a
	}
	if p.Status == state.Evaluating {
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
	if ap.Proposals > 0 {
		a.Phase, a.Reason = state.PhaseTriage, fmt.Sprintf("仕分けていない提案が %d 件", ap.Proposals)
		return a
	}
	if r := patrolDue(s, p); r != "" {
		a.Phase, a.Role, a.Reason = state.PhasePatrol, r, fmt.Sprintf("%s の見回りから %d 日以上", r, c.PatrolEveryDays)
		return a
	}
	for _, is := range sorted(ap.Issues) {
		if !is.Blocked() {
			a.Phase, a.Issue, a.Fix, a.Role = state.PhaseBuild, is.Number, is.Fix(), role.ForIssue(is.Labels)
			a.Issues = batch(ap.Issues, is)
			a.Reason = fmt.Sprintf("未完了の issue #%d (%d/%d tick)", is.Number, p.Ticks.Phase, limit)
			return a
		}
	}
	a.Phase = state.PhaseEvaluate
	if len(ap.Issues) > 0 {
		a.Reason = "残りの issue が全部 blocked"
	} else {
		a.Reason = "マイルストーン " + p.Milestone + " の issue が全部閉じた"
	}
	return a
}

// patrolDue は見回りの番のロールを返す (なければ空)。
// 見回るのは、アプリがある程度進んでから (見るものがない)。順番は role.Workers (安全が先)。
func patrolDue(s Snapshot, p *state.Project) string {
	c := s.Cfg
	if c.PatrolEveryDays <= 0 || p.Ticks.Total < c.PatrolMinTicks {
		return ""
	}
	for _, r := range role.Workers() {
		if state.DaysSince(s.Meta.LastPatrol[r], s.Now) >= c.PatrolEveryDays {
			return r
		}
	}
	return ""
}

// sorted は解く順: 直す issue (wk:fix) が先、その中は番号の小さい順。
func sorted(is []Issue) []Issue {
	out := slices.Clone(is)
	slices.SortFunc(out, func(a, b Issue) int {
		if a.Fix() != b.Fix() {
			if a.Fix() {
				return -1
			}
			return 1
		}
		return a.Number - b.Number
	})
	return out
}

// batch は first と一緒に作る issue。直す issue はそれだけで解く。ふつうの issue は、ブロックされていないものを全部まとめる
func batch(is []Issue, first Issue) []int {
	if first.Fix() {
		return []int{first.Number}
	}
	var out []int
	for _, i := range sorted(is) {
		if !i.Blocked() && !i.Fix() {
			out = append(out, i.Number)
		}
	}
	return out
}

// decideFocus は主力のアプリだけを進める。まだ portfolio になければ (土台を人の側が作っている) 待つ。
func decideFocus(s Snapshot, apps []App) Action {
	for _, ap := range apps {
		if ap.Project.Idea != s.Cfg.Focus {
			continue
		}
		if s.claimed(ap.Project) {
			return Action{Phase: state.PhaseIdle, Reason: "主力のアプリ (" + s.Cfg.Focus + ") はほかの実行が進めている"}
		}
		return decideApp(s, ap)
	}
	return Action{Phase: state.PhaseIdle, Reason: "主力のアプリ (" + s.Cfg.Focus + ") の土台を待っている (作りかけにない)"}
}
