package next

// ロールのループ (docs/ROLES.md): 担当のロール・見回り (patrol)・仕分け (triage) の決まり。

import (
	"testing"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/role"
	"github.com/Rin0107/ideas/internal/state"
)

// 見回りを有効にした設定: 3 日おき、アプリに 5 tick 使ってから
func patrolCfg() *config.Config {
	c := cfg()
	c.PatrolEveryDays, c.PatrolMinTicks = 3, 5
	return c
}

// 3 ロールとも昨日見回った
func patrolled(day string) *state.Meta {
	m := meta()
	m.LastPatrol = map[string]string{role.SRE: day, role.Designer: day, role.Engineer: day}
	return m
}

func TestDecideRoles(t *testing.T) {
	iterating := func(ticks int) *state.Project { p := building(ticks); p.Status = state.Iterating; return p }
	with := func(m *state.Meta, r, day string) *state.Meta { m.LastPatrol[r] = day; return m }
	cases := []struct {
		name  string
		cfg   *config.Config
		s     Snapshot
		phase string
		role  string
		issue int
	}{
		// 担当: build は issue のラベル、ship は SRE、ほかは leader
		{"build: ラベルなしは engineer", patrolCfg(),
			Snapshot{Active: building(6), Meta: patrolled("2026-11-09"), Issues: []Issue{{Number: 2}}}, state.PhaseBuild, role.Engineer, 2},
		{"build: role:designer の issue は designer", patrolCfg(),
			Snapshot{Active: building(6), Meta: patrolled("2026-11-09"), Issues: []Issue{{Number: 2, Labels: []string{"role:designer"}}}},
			state.PhaseBuild, role.Designer, 2},
		{"build: 選ばれた issue のラベルで決まる (ほかの issue のラベルではない)", patrolCfg(),
			Snapshot{Active: building(6), Meta: patrolled("2026-11-09"),
				Issues: []Issue{{Number: 7, Labels: []string{"role:designer"}}, {Number: 4, Labels: []string{"role:sre"}}}},
			state.PhaseBuild, role.SRE, 4},
		{"ship は SRE", cfg(),
			Snapshot{Active: func() *state.Project { p := building(10); p.Status, p.Verdict = state.Evaluating, "ship"; return p }()},
			state.PhaseShip, role.SRE, 0},
		{"evaluate は leader (designer と SRE に採点を頼む)", cfg(),
			Snapshot{Active: building(10)}, state.PhaseEvaluate, role.Leader, 0},
		{"select は leader", cfg(), Snapshot{Untried: 500}, state.PhaseSelect, role.Leader, 0},

		// 仕分け: 提案があれば build より先
		{"提案がある → triage (leader)", patrolCfg(),
			Snapshot{Active: building(6), Meta: patrolled("2026-11-09"), Proposals: 1, Issues: []Issue{{Number: 2}}},
			state.PhaseTriage, role.Leader, 0},
		{"提案があっても時間切れなら evaluate", patrolCfg(),
			Snapshot{Active: building(30), Meta: patrolled("2026-11-09"), Proposals: 2}, state.PhaseEvaluate, role.Leader, 0},
		{"提案があれば、見回りの番でも先に triage", patrolCfg(),
			Snapshot{Active: building(6), Proposals: 1}, state.PhaseTriage, role.Leader, 0},
		{"見回りを止めていても (0 日)、提案があれば triage", cfg(),
			Snapshot{Active: building(6), Proposals: 3}, state.PhaseTriage, role.Leader, 0},

		// 見回り: SRE → designer → engineer の順。間隔は 3 日 (境界: 3 日前は番、2 日前はまだ)
		{"一度も見回っていない → SRE から", patrolCfg(),
			Snapshot{Active: building(5), Issues: []Issue{{Number: 2}}}, state.PhasePatrol, role.SRE, 0},
		{"SRE は済み → designer", patrolCfg(),
			Snapshot{Active: building(5), Meta: &state.Meta{LastIntake: "2026-11-09", LastRetro: "2026-11-09",
				LastPatrol: map[string]string{role.SRE: "2026-11-10"}}}, state.PhasePatrol, role.Designer, 0},
		{"SRE と designer は済み → engineer", patrolCfg(),
			Snapshot{Active: building(5), Meta: with(with(patrolled("2026-11-10"), role.Engineer, "2026-11-07"), role.SRE, "2026-11-08")},
			state.PhasePatrol, role.Engineer, 0},
		{"3 日前 (ちょうど) は番", patrolCfg(),
			Snapshot{Active: building(5), Meta: with(patrolled("2026-11-09"), role.Designer, "2026-11-07"), Issues: []Issue{{Number: 2}}},
			state.PhasePatrol, role.Designer, 0},
		{"2 日前はまだ → build", patrolCfg(),
			Snapshot{Active: building(5), Meta: patrolled("2026-11-08"), Issues: []Issue{{Number: 2}}}, state.PhaseBuild, role.Engineer, 2},
		{"iterating でも見回る", patrolCfg(),
			Snapshot{Active: iterating(5), Issues: []Issue{{Number: 2}}}, state.PhasePatrol, role.SRE, 0},
		{"issue が全部閉じていても、評価の前に見回る", patrolCfg(),
			Snapshot{Active: building(9)}, state.PhasePatrol, role.SRE, 0},
		// アプリに 5 tick 使うまでは見回らない (境界: 4 はまだ、5 から)
		{"4 tick ではまだ見回らない", patrolCfg(),
			Snapshot{Active: building(4), Issues: []Issue{{Number: 2}}}, state.PhaseBuild, role.Engineer, 2},
		{"見回りを止めている (0 日) → build", cfg(),
			Snapshot{Active: building(6), Issues: []Issue{{Number: 2}}}, state.PhaseBuild, role.Engineer, 2},
		{"間隔が負でも見回らない", func() *config.Config { c := patrolCfg(); c.PatrolEveryDays = -1; return c }(),
			Snapshot{Active: building(6), Issues: []Issue{{Number: 2}}}, state.PhaseBuild, role.Engineer, 2},
		// 時間切れ・評価中・選定中は見回らない
		{"時間切れなら見回らず evaluate", patrolCfg(),
			Snapshot{Active: building(30)}, state.PhaseEvaluate, role.Leader, 0},
		{"評価中は見回らない", patrolCfg(),
			Snapshot{Active: func() *state.Project { p := building(9); p.Status = state.Evaluating; return p }(), Proposals: 2},
			state.PhaseEvaluate, role.Leader, 0},
		{"選定の途中は見回らない", patrolCfg(),
			Snapshot{Active: func() *state.Project { p := building(9); p.Status = state.Selected; return p }(), Proposals: 2},
			state.PhaseSelect, role.Leader, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := c.s
			s.Now, s.Cfg = now, c.cfg
			if s.Meta == nil {
				s.Meta = meta()
			}
			got := Decide(s)
			if got.Phase != c.phase || got.Role != c.role || got.Issue != c.issue || got.Reason == "" {
				t.Fatalf("got %s (%s) #%d [%s], want %s (%s) #%d", got.Phase, got.Role, got.Issue, got.Reason, c.phase, c.role, c.issue)
			}
		})
	}
}

// 理由の文に、数と間隔が正しく入る
func TestRoleReasons(t *testing.T) {
	s := Snapshot{Now: now, Cfg: patrolCfg(), Meta: meta(), Active: building(6), Proposals: 2}
	if a := Decide(s); a.Reason != "仕分けていない提案が 2 件" {
		t.Fatal(a.Reason)
	}
	s.Proposals = 0
	if a := Decide(s); a.Reason != "sre の見回りから 3 日以上" {
		t.Fatal(a.Reason)
	}
}
