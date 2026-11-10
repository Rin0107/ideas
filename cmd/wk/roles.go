package main

// ロールが自分で仕事を見つける仕組み (docs/ROLES.md):
//   wk propose: 見回り (patrol) の中で、ロールが提案を issue にする
//   wk triage:  leader が提案を仕分ける (今やる・あとで・やらない)
// 数の上限と行き先は wk が決める (LLM が勝手に issue を増やせない)。

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/role"
	"github.com/Rin0107/ideas/internal/state"
	"github.com/Rin0107/ideas/internal/verdict"
)

const (
	labelProposal = "wk:proposal" // 見回りで出た、まだ仕分けていない提案
	labelLater    = "wk:later"    // あとで (次のブラッシュアップの計画の材料)
)

// roleOr は r が空なら def を返す。
func roleOr(r, def string) string {
	if r == "" {
		return def
	}
	return r
}

// working は、作っている途中 (issue を足してよい状態) のアプリを返す。
func (e *env) working() (*state.Project, error) {
	p, err := e.project()
	if err != nil {
		return nil, err
	}
	if p.Status != state.Building && p.Status != state.Iterating {
		return nil, fmt.Errorf("作っている途中でない (%s)", p.Status)
	}
	return p, nil
}

// ---- propose ----

type proposals struct {
	Issues []PlanIssue `json:"issues"`
}

func cmdPropose(args []string) error {
	var planPath *string
	if _, err := flags("propose", args, func(f *flag.FlagSet) { planPath = f.String("plan", "", "提案の JSON ({\"issues\": [...]})") }); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	p, err := e.working()
	if err != nil {
		return err
	}
	// 提案できるのは、wk next が見回りを割り当てた tick の中だけ。誰の提案かもそこから決まる
	a, _, err := readCurrent(e)
	if err != nil {
		return err
	}
	if a.Phase != state.PhasePatrol || !role.Valid(a.Role) || a.Repo != p.Repo {
		return fmt.Errorf("提案は見回り (patrol) の tick の中だけ (今は %s)", a.Phase)
	}
	var ps proposals
	if err := verdict.ReadJSON(*planPath, &ps); err != nil {
		return err
	}
	if len(ps.Issues) > e.cfg.MaxProposals {
		return fmt.Errorf("提案は 1 回の見回りで %d 件まで (今 %d 件)。大事なものだけにする", e.cfg.MaxProposals, len(ps.Issues))
	}
	if errs := issueErrors(ps.Issues); len(errs) > 0 {
		return errors.New("提案の形が不正:\n  " + strings.Join(errs, "\n  "))
	}
	if len(ps.Issues) == 0 {
		fmt.Printf("%s の見回り: 提案なし\n", a.Role)
		return nil
	}
	if err := ensureLabels(p.Repo, e.cfg.MaxAttemptsPerIssue); err != nil {
		return err
	}
	for _, is := range ps.Issues {
		body := issueBody(is) + fmt.Sprintf("\n提案: %s の見回り (%s)\n", a.Role, e.today())
		if _, err := gh.GH("issue", "create", "--repo", p.Repo, "--title", is.Title, "--body", body,
			"--label", labelProposal, "--label", role.Label(roleOr(is.Role, a.Role))); err != nil {
			return err
		}
	}
	fmt.Printf("%s の見回り: 提案 %d 件 (次の tick で leader が仕分ける)\n", a.Role, len(ps.Issues))
	return nil
}

// ---- triage ----

type decision struct {
	Issue    int    `json:"issue"`
	Decision string `json:"decision"` // now / later / reject
	Reason   string `json:"reason"`
}

func cmdTriage(args []string) error {
	var path *string
	if _, err := flags("triage", args, func(f *flag.FlagSet) {
		path = f.String("decisions", "", "仕分けの JSON ([{issue, decision, reason}])")
	}); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	p, err := e.working()
	if err != nil {
		return err
	}
	var ds []decision
	if err := verdict.ReadJSON(*path, &ds); err != nil {
		return err
	}
	open, err := gh.OpenWithLabel(p.Repo, labelProposal)
	if err != nil {
		return err
	}
	// 形の検査: 今ある提案の全部に、ちょうど 1 つずつ、理由つきの結論があること
	byIssue := map[int]decision{}
	var errs []string
	now := 0
	for i, d := range ds {
		if d.Decision != "now" && d.Decision != "later" && d.Decision != "reject" {
			errs = append(errs, fmt.Sprintf("[%d] #%d: decision は now / later / reject", i, d.Issue))
		}
		if len([]rune(strings.TrimSpace(d.Reason))) < 5 {
			errs = append(errs, fmt.Sprintf("[%d] #%d: reason が短すぎる", i, d.Issue))
		}
		if _, dup := byIssue[d.Issue]; dup {
			errs = append(errs, fmt.Sprintf("[%d] #%d: 同じ issue が 2 回ある", i, d.Issue))
		}
		byIssue[d.Issue] = d
		if d.Decision == "now" && slices.Contains(open, d.Issue) {
			now++
		}
	}
	for _, n := range open {
		if _, ok := byIssue[n]; !ok {
			errs = append(errs, fmt.Sprintf("#%d: 結論がない (提案は全部仕分ける)", n))
		}
	}
	if len(errs) > 0 {
		return errors.New("仕分けの形が不正:\n  " + strings.Join(errs, "\n  "))
	}
	// 今やるものは、今のマイルストーンに入る。open な issue の上限を超えるなら、あとでに回させる
	cur, err := gh.OpenIssues(p.Repo, p.Milestone)
	if err != nil {
		return err
	}
	if len(cur)+now > e.cfg.MaxOpenIssues {
		return fmt.Errorf("今やるにすると open な issue が %d 件になる (上限 %d)。いくつかを later にする", len(cur)+now, e.cfg.MaxOpenIssues)
	}
	count := map[string]int{}
	for _, n := range open {
		d := byIssue[n]
		num := fmt.Sprint(n)
		comment := fmt.Sprintf("wakuwaku leader: %s — %s", d.Decision, strings.TrimSpace(d.Reason))
		var err error
		if d.Decision == "now" {
			_, err = gh.GH("issue", "edit", num, "--repo", p.Repo, "--milestone", p.Milestone,
				"--remove-label", labelProposal, "--add-label", "wk:task")
		}
		if d.Decision == "later" {
			_, err = gh.GH("issue", "edit", num, "--repo", p.Repo, "--remove-label", labelProposal, "--add-label", labelLater)
		}
		if d.Decision == "reject" {
			_, err = gh.GH("issue", "close", num, "--repo", p.Repo, "--reason", "not planned")
		}
		if err != nil {
			return err
		}
		if _, err := gh.GH("issue", "comment", num, "--repo", p.Repo, "--body", comment); err != nil {
			return err
		}
		count[d.Decision]++
	}
	fmt.Printf("仕分けた: 今やる %d・あとで %d・やらない %d\n", count["now"], count["later"], count["reject"])
	return nil
}
