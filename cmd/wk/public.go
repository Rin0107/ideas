package main

// 工場のアプリの repo は、作っている間から public にする (持ち主の指定 2026-11-28: private の Actions の無料の分を使い切った)。
// その代わりに次の 3 つを守る:
//   - 個人を特定できるもの (名前・メール・家のパス) を git にも issue・PR の文にも入れない
//     (git は pre-push と CI の privacy-scan、文は guard と wk publicize の検査)
//   - ほかの人の PR は review も merge もしない (prOwnProblem)。fork からの PR の CI は持ち主の承認なしに動かさない
//   - ほかの人のコメントをエージェントに読ませない (wk comments は持ち主のものだけを出す)

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/guard"
)

// securePublic は public の repo に、鍵の push を GitHub の側で止める設定 (secret scanning と push protection) と、
// 外の人の PR の CI を持ち主が承認するまで動かさない設定を入れる。
func securePublic(repo string) error {
	if _, err := gh.GH("api", "-X", "PATCH", "repos/"+repo,
		"-f", "security_and_analysis[secret_scanning][status]=enabled",
		"-f", "security_and_analysis[secret_scanning_push_protection][status]=enabled"); err != nil {
		return err
	}
	_, err := gh.GH("api", "-X", "PUT", "repos/"+repo+"/actions/permissions/fork-pr-contributor-approval",
		"-f", "approval_policy=all_external_contributors")
	return err
}

// prOwnProblem は、持ち主のでない PR (ほかの人が出した・fork から来た) を断る理由 (よければ "")。
// public の repo には誰でも PR を出せる。工場はほかの人の PR を review も merge もしない (持ち主の指定 2026-11-28)
func prOwnProblem(e *env, repo string, pr int) (string, error) {
	out, err := gh.GH("pr", "view", fmt.Sprint(pr), "--repo", repo, "--json", "author,isCrossRepository",
		"--jq", `.author.login + " " + (.isCrossRepository | tostring)`)
	if err != nil {
		return "", err
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return "", fmt.Errorf("PR の作者を読めない: %q", strings.TrimSpace(out))
	}
	if f[1] != "false" {
		return "fork から来た PR は review も merge もしない (持ち主の PR だけ)", nil
	}
	if !strings.EqualFold(f[0], e.cfg.Owner) {
		return fmt.Sprintf("持ち主 (%s) でない人 (%s) の PR は review も merge もしない", e.cfg.Owner, f[0]), nil
	}
	return "", nil
}

type ghComment struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

// cmdComments は issue (か PR) の題と本文と、持ち主が書いたコメントだけを出す。
// public の repo では誰でもコメントを書けるので、エージェントは gh の --comments でなくこれで読む (guard が止める)
func cmdComments(args []string) error {
	fs := flag.NewFlagSet("comments", flag.ContinueOnError)
	repo := fs.String("repo", "", "owner/name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// 番号は --repo の前でも後ろでもよい
	pos := fs.Args()
	if len(pos) > 0 {
		if err := fs.Parse(pos[1:]); err != nil {
			return err
		}
		pos = append(pos[:1], fs.Args()...)
	}
	var num int
	if len(pos) != 1 || *repo == "" {
		return errors.New("使い方: wk comments <issue> --repo <owner/name>")
	} else if _, err := fmt.Sscan(pos[0], &num); err != nil || num <= 0 {
		return fmt.Errorf("issue の番号が違う: %q", pos[0])
	}
	e, err := load()
	if err != nil {
		return err
	}
	out, err := gh.GH("issue", "view", fmt.Sprint(num), "--repo", *repo, "--json", "title,body,author,comments")
	if err != nil {
		return err
	}
	var v struct {
		ghComment
		Title    string      `json:"title"`
		Comments []ghComment `json:"comments"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return fmt.Errorf("issue を読めない: %w", err)
	}
	own := func(login string) bool { return strings.EqualFold(login, e.cfg.Owner) }
	fmt.Printf("#%d %s\n\n", num, v.Title)
	if own(v.Author.Login) {
		fmt.Println(v.Body)
	} else {
		fmt.Println("(本文は持ち主でない人が書いたので出さない)")
	}
	skipped := 0
	for _, c := range v.Comments {
		if !own(c.Author.Login) {
			skipped++
			continue
		}
		fmt.Printf("\n--- %s\n%s\n", c.CreatedAt, c.Body)
	}
	if skipped > 0 {
		fmt.Printf("\n(持ち主でない人のコメント %d 件は出さない)\n", skipped)
	}
	return nil
}

// cmdPublicize は作っているアプリの repo を public にする (人だけ。guard がエージェントを止める)。
// 先に全履歴 (PR の ref も) と、issue・PR・コメント・レビューの文を全部見て、個人情報があれば public にしない
func cmdPublicize(args []string) error {
	fs := flag.NewFlagSet("publicize", flag.ContinueOnError)
	repo := fs.String("repo", "", "owner/name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repo == "" {
		return errors.New("使い方: wk publicize --repo <owner/name>")
	}
	e, err := load()
	if err != nil {
		return err
	}
	if !e.pf.Repos()[*repo] {
		return fmt.Errorf("%s は portfolio にない", *repo)
	}
	vis, err := gh.GH("repo", "view", *repo, "--json", "visibility", "--jq", ".visibility")
	if err != nil {
		return err
	}
	if strings.TrimSpace(vis) == "PUBLIC" {
		fmt.Printf("%s はもう public。設定だけ入れ直す\n", *repo)
		return securePublic(*repo)
	}
	probs := privacyScan(e, *repo)
	probs = append(probs, textScan(e, *repo)...)
	if len(probs) > 0 {
		return errors.New("public にしない (個人情報の検査に通らない):\n  " + strings.Join(probs, "\n  "))
	}
	if _, err := gh.GH("repo", "edit", *repo, "--visibility", "public", "--accept-visibility-change-consequences"); err != nil {
		return err
	}
	if err := securePublic(*repo); err != nil {
		return err
	}
	fmt.Printf("public にした: %s (secret scanning・push protection・外の人の PR の CI は承認制)\n", *repo)
	return nil
}

// textScanSources は public にする前に見る文 (gh api の道と、取り出す欄)。PR も issue の一覧に入る
var textScanSources = [][2]string{
	{"issues?state=all&per_page=100", `.[] | .title, (.body // "")`},
	{"issues/comments?per_page=100", `.[] | .body // ""`},
	{"pulls/comments?per_page=100", `.[] | .body // ""`},
	{"releases?per_page=100", `.[] | .name // "", (.body // "")`},
}

// textScan は issue・PR・コメント・レビュー・リリースの文を集めて、工場の privacy-scan の text で見る。
var textScan = func(e *env, repo string) []string {
	var all strings.Builder
	for _, s := range textScanSources {
		out, err := gh.GH("api", "--paginate", "repos/"+repo+"/"+s[0], "--jq", s[1])
		if err != nil {
			return []string{err.Error()}
		}
		all.WriteString(out)
		all.WriteString("\n")
	}
	// 置き場所は runtime_dir (guard でエージェントは触れない)。見終わったら消す
	file := filepath.Join(e.cfg.RuntimeDir, "publicize-text.txt")
	if err := os.WriteFile(file, []byte(all.String()), 0o600); err != nil {
		return []string{err.Error()}
	}
	defer os.Remove(file)
	if _, err := gh.Run("", "node", filepath.Join(e.factoryMain(), "scripts", "privacy-scan.mjs"), "text", file); err != nil {
		return []string{"issue・PR の文に個人情報がある: " + scanMessage(err)}
	}
	return nil
}

// scanMessage は検査の失敗から、検査の出力 (場所と種類だけ。値は出さない) を取り出す。
// 実行したコマンドの文字列には家のパスが入るので、public の issue に書けない
func scanMessage(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "NG: "); i >= 0 {
		return msg[i:]
	}
	return "検査が動かない (詳しくは手元で wk audit を実行して見る)"
}

// cmdAudit は毎日の見回り (Orca の automation の precheck から。エージェントは使わない)。
// 持ち主の public の repo を全部 (fork は除く。orca は持ち主が「そのまま」と決めた)、全履歴・PR の ref・issue と PR の文まで見る。
// 見つかったら、工場のアプリは private に戻し、どれも人に知らせる (持ち主の指定 2026-11-28:「二度と起こらないように」)
func cmdAudit(args []string) error {
	if len(args) > 0 {
		return errors.New("使い方: wk audit")
	}
	e, err := load()
	if err != nil {
		return err
	}
	out, err := gh.GH("repo", "list", e.cfg.Owner, "--visibility", "public", "--limit", "200",
		"--json", "nameWithOwner,isFork", "--jq", ".[] | select(.isFork | not) | .nameWithOwner")
	if err != nil {
		return err
	}
	var found []string
	for _, r := range strings.Fields(out) {
		probs := append(privacyScan(e, r), textScan(e, r)...)
		if len(probs) == 0 {
			fmt.Println("ok:", r)
			continue
		}
		line := r + ": " + strings.Join(probs, " / ")
		if e.pf.Repos()[r] {
			if _, err := gh.GH("repo", "edit", r, "--visibility", "private", "--accept-visibility-change-consequences"); err != nil {
				line += " (private に戻せない)"
			} else {
				line += " → private に戻した"
			}
		}
		found = append(found, line)
	}
	if len(found) == 0 {
		fmt.Println("見回り: 個人情報なし")
		return nil
	}
	body := "毎日の見回り (wk audit) で、public の repo に個人情報が見つかった。値は書かない (この issue も public)。\n\n- " +
		strings.Join(found, "\n- ") + "\n\n工場のアプリは private に戻した。ほかの repo は人が直す。手元で `wk audit` を実行すると同じものが出る。"
	if hit := guard.Personal(body); len(hit) > 0 {
		body = "毎日の見回り (wk audit) で、public の repo に個人情報が見つかった。詳しくは手元で `wk audit` を実行して見る (知らせの文にも個人情報が入るので書かない)。"
	}
	if err := openHuman(e, "public の repo に個人情報 (wk audit)", body); err != nil {
		return err
	}
	return errors.New("見回り: 個人情報が見つかった:\n  " + strings.Join(found, "\n  "))
}
