package main

// 自分で直す仕組み (docs/DESIGN.md §9)。tick の結果は 4 つ:
//   ok:    進んだ
//   retry: issue の試行が通らなかった (wk attempt で issue に記録済み)。失敗に数えず、次の tick がもう一度試す
//   fix:   人の手なしで直せる問題に当たった。失敗に数えない。直す issue (wk:fix) を立て、次の tick で最優先に解く
//   human: 人がやらないといけない (守りの部分・鍵・アカウント・お金)。失敗に数え、人宛ての issue (wk:human) を立てる。
//          工場 repo の notify ワークフローが持ち主に @メンションするので、GitHub からメールが届く (鍵は要らない)
// 同じ wk:fix の issue を max_attempts_per_issue 回試しても直らなければ human に上げる (直す issue が無限に続かない)。

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/next"
	"github.com/Rin0107/ideas/internal/role"
)

const labelHuman = "wk:human"

var healLabels = map[string]string{next.LabelFix: "0e8a16", labelHuman: "d93f0b"}

// ensureLabel は repo にラベルを作る (あれば何もしない)。
func ensureLabel(repo, name string) error {
	_, err := gh.GH("label", "create", name, "--repo", repo, "--color", healLabels[name], "--force")
	return err
}

// readDetail は --detail のファイル (なければ空)。
func readDetail(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("--detail を読めない: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func tickContext(a next.Action) string {
	if a.Phase == "" {
		return "wk fix から (tick の外)"
	}
	return fmt.Sprintf("tick: %s (担当 %s) %s #%d", a.Phase, a.Role, a.Repo, a.Issue)
}

// openHuman は人宛ての issue を工場 repo に立てる (notify ワークフローがメールで知らせる)。
func openHuman(e *env, title, body string) error {
	repo := e.factoryRepo()
	if err := ensureLabel(repo, labelHuman); err != nil {
		return err
	}
	_, err := gh.GH("issue", "create", "--repo", repo, "--title", "wk:human "+title, "--label", labelHuman, "--body",
		body+"\n\n---\n終わったら、この issue を閉じる (止まっている issue があれば wk:blocked を外す)。")
	return err
}

// bumpFix は wk:fix の issue に試行を 1 つ足す。上限に達したら blocked にして true を返す (人に上げる)。
func bumpFix(e *env, repo string, is next.Issue, note string) (bool, error) {
	n := is.Attempts() + 1
	add := fmt.Sprintf("wk:attempt-%d", n)
	full := n >= e.cfg.MaxAttemptsPerIssue
	if full {
		add += "," + next.LabelBlocked
	}
	if _, err := gh.GH("label", "create", fmt.Sprintf("wk:attempt-%d", n), "--repo", repo, "--color", "fbca04", "--force"); err != nil {
		return false, err
	}
	if _, err := gh.GH("issue", "edit", fmt.Sprint(is.Number), "--repo", repo, "--add-label", add); err != nil {
		return false, err
	}
	_, err := gh.GH("issue", "comment", fmt.Sprint(is.Number), "--repo", repo, "--body",
		fmt.Sprintf("wakuwaku factory: 直す試行 %d/%d が通らなかった。\n\n%s", n, e.cfg.MaxAttemptsPerIssue, note))
	return full, err
}

// fixTarget は直す issue を立てる repo とマイルストーン。アプリの tick ならアプリ、それ以外 (と --in factory) は工場。
func fixTarget(e *env, a next.Action, in string) (string, string) {
	if p := e.pf.Active(); in != "factory" && p != nil && a.Repo == p.Repo && a.Repo != "" {
		return p.Repo, p.Milestone
	}
	return e.factoryRepo(), ""
}

// openFixes は repo の open な wk:fix の issue (マイルストーンが空なら全部)。
func openFixes(repo, milestone string) ([]next.Issue, error) {
	var all []next.Issue
	var err error
	if milestone == "" {
		all, err = gh.LabeledIssues(repo, next.LabelFix)
	} else {
		all, err = gh.OpenIssues(repo, milestone)
	}
	if err != nil {
		return nil, err
	}
	var out []next.Issue
	for _, is := range all {
		if is.Fix() {
			out = append(out, is)
		}
	}
	return out, nil
}

// handleFix は result fix の後始末。人に上げることになったら true を返す。
func handleFix(e *env, a next.Action, note, detail, in string) (bool, error) {
	if a.Fix { // 直す issue そのものが直らなかった: 新しく立てずに試行を数える
		is, err := currentIssue(a)
		if err != nil {
			return false, err
		}
		return bumpFix(e, a.Repo, is, note+"\n\n"+detail)
	}
	repo, ms := fixTarget(e, a, in)
	title := "wk:fix " + note
	open, err := openFixes(repo, ms)
	if err != nil {
		return false, err
	}
	for _, is := range open {
		if is.Title == title { // 同じ問題がもう立っている: 試行を数える
			return bumpFix(e, repo, is, detail)
		}
	}
	if err := ensureLabel(repo, next.LabelFix); err != nil {
		return false, err
	}
	args := []string{"issue", "create", "--repo", repo, "--title", title, "--label", next.LabelFix,
		"--label", role.Label(fixOwner(a)), "--body", "wakuwaku factory が自分で直す問題。\n\n" + tickContext(a) + "\n\n" + detail}
	if ms != "" {
		args = append(args, "--milestone", ms)
	}
	_, err = gh.GH(args...)
	return false, err
}

// fixOwner は直す issue の担当。手を動かすロールならそのロール、leader の段なら sre (信頼性の持ち主)。
func fixOwner(a next.Action) string {
	if role.Valid(a.Role) {
		return a.Role
	}
	return role.SRE
}

// currentIssue は今の tick の issue のラベルを読み直す (試行の数を正しく数えるため)。
func currentIssue(a next.Action) (next.Issue, error) {
	out, err := gh.LabeledIssues(a.Repo, next.LabelFix)
	if err != nil {
		return next.Issue{}, err
	}
	for _, is := range out {
		if is.Number == a.Issue {
			return is, nil
		}
	}
	return next.Issue{}, fmt.Errorf("%s #%d が open な wk:fix にない", a.Repo, a.Issue)
}

// handleHuman は result human の後始末: 人宛ての issue を立て、今の issue を止める。
func handleHuman(e *env, a next.Action, note, detail string) error {
	if err := openHuman(e, note, "工場が人の作業を待っている。\n\n"+tickContext(a)+"\n\n"+detail); err != nil {
		return err
	}
	if a.Issue == 0 || a.Repo == "" {
		return nil
	}
	_, err := gh.GH("issue", "edit", fmt.Sprint(a.Issue), "--repo", a.Repo, "--add-label", next.LabelBlocked)
	return err
}

// cmdHuman は tick を終えずに人へ頼む (retro の「人に頼みたいこと」など)。失敗には数えない。
func cmdHuman(args []string) error {
	fs := flag.NewFlagSet("human", flag.ContinueOnError)
	title := fs.String("title", "", "1 行の題")
	detail := fs.String("detail", "", "本文のファイル (やってほしいこと・手順・差分)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *title == "" {
		return errors.New("--title が要る")
	}
	body, err := readDetail(*detail)
	if err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	if err := openHuman(e, *title, body); err != nil {
		return err
	}
	fmt.Println("人に頼んだ:", *title)
	return nil
}

// cmdFix は tick を終えずに、工場の直す issue を立てる (見回り・振り返りで見つけた工場の問題)。失敗には数えない。
func cmdFix(args []string) error {
	fs := flag.NewFlagSet("fix", flag.ContinueOnError)
	title := fs.String("title", "", "1 行の題")
	detail := fs.String("detail", "", "本文のファイル (何が起きたか・原因・どう直すか)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *title == "" {
		return errors.New("--title が要る")
	}
	body, err := readDetail(*detail)
	if err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	if _, err := handleFix(e, next.Action{Role: role.SRE}, *title, body, "factory"); err != nil {
		return err
	}
	fmt.Println("直す issue にした:", *title)
	return nil
}
