// Package role は工場のロール (役割) と、誰が何を担当するかの決まり。docs/ROLES.md
//
//   - leader:   指示を出す。段階ごとに担当のロールへ仕事を渡し、結果だけを受け取る (選ぶ・計画する・仕分ける)
//   - engineer: 設計と実装 (クリーンアーキテクチャ・ドメイン駆動設計)
//   - designer: デザインシステムの一貫性と UX
//   - sre:      QA・信頼性・セキュリティ
//
// 決まりは純粋関数にして、テストで固定する (LLM が決めない)。
package role

import (
	"slices"
	"strings"

	"github.com/Rin0107/ideas/internal/state"
)

const (
	Leader   = "leader"
	Engineer = "engineer"
	Designer = "designer"
	SRE      = "sre"
)

const labelPrefix = "role:"

// Workers は手を動かすロール (leader 以外)。見回りはこの順に回る (安全が先)。
func Workers() []string { return []string{SRE, Designer, Engineer} }

// Valid は手を動かすロールの名前か。
func Valid(r string) bool { return slices.Contains(Workers(), r) }

// Label は issue に付けるラベル。
func Label(r string) string { return labelPrefix + r }

// ForIssue は issue の担当。role:<名前> のラベルで決まり、なければ engineer。
func ForIssue(labels []string) string {
	for _, r := range Workers() {
		if slices.Contains(labels, Label(r)) {
			return r
		}
	}
	return Engineer
}

// ForPhase は段階の担当 (build と patrol は issue・順番で決まるので、ここでは扱わない)。
// 公開 (ship) は SRE の責任。それ以外 (選ぶ・計画する・仕分ける・評価をまとめる・振り返る) は leader。
func ForPhase(phase string) string {
	if phase == state.PhaseShip {
		return SRE
	}
	return Leader
}

// TouchesUI は、変更が利用者の目に触れる部分 (画面・CLI の表示・デザインシステム) を含むか。
// 含むなら designer のレビューが要る。
func TouchesUI(files []string) bool {
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "index.html" || f == "docs/design-system.md" || strings.HasPrefix(f, "public/") || strings.HasPrefix(f, "cmd/") {
			return true
		}
		if strings.HasPrefix(f, "src/") && !strings.HasPrefix(f, "src/worker/") {
			return true
		}
	}
	return false
}

// Reviewers は、その PR を通すのに approve が要るロール (この順にレビューする)。
// SRE (セキュリティと品質) と engineer (受け入れ条件と設計) はいつも。designer は UI に触れたときだけ。
func Reviewers(files []string) []string {
	rs := []string{SRE, Engineer}
	if TouchesUI(files) {
		rs = append(rs, Designer)
	}
	return rs
}
