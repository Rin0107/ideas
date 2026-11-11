package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/guard"
	"github.com/Rin0107/ideas/internal/role"
)

const maxDiffBytes = 150_000

// ---- review: 別の Claude に PR を見せる ----

type review struct {
	Role    string   `json:"role"`    // レビューしたロール (sre / engineer / designer)
	Verdict string   `json:"verdict"` // approve / request_changes
	Issues  []string `json:"issues"`
	Summary string   `json:"summary"`
}

// reviews は wk review の結果。verdict は全員が approve のときだけ approve。
type reviews struct {
	Verdict string   `json:"verdict"`
	Reviews []review `json:"reviews"`
}

func cmdReview(args []string) error {
	repo, pr, err := repoPR(args)
	if err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	if !e.pf.Repos()[repo] {
		return fmt.Errorf("%s は portfolio にない", repo)
	}
	headSHA, err := prHead(repo, pr)
	if err != nil {
		return err
	}
	diff, err := gh.GH("pr", "diff", fmt.Sprint(pr), "--repo", repo)
	if err != nil {
		return err
	}
	if len(diff) > maxDiffBytes {
		return fmt.Errorf("差分が大きすぎる (%d バイト)。issue を分ける", len(diff))
	}
	view, err := gh.GH("pr", "view", fmt.Sprint(pr), "--repo", repo, "--json", "title,body,closingIssuesReferences")
	if err != nil {
		return err
	}
	var v struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		Closing []struct {
			Number int `json:"number"`
		} `json:"closingIssuesReferences"`
	}
	if err := json.Unmarshal([]byte(view), &v); err != nil {
		return err
	}
	if len(v.Closing) == 0 {
		return errors.New("PR に Closes #N がない")
	}
	var issues strings.Builder
	for _, c := range v.Closing {
		body, err := gh.GH("issue", "view", fmt.Sprint(c.Number), "--repo", repo, "--json", "number,title,body",
			"--jq", `"#\(.number) \(.title)\n\(.body)"`)
		if err != nil {
			return err
		}
		issues.WriteString(body + "\n\n")
	}
	checks, _ := gh.Checks(repo, pr)
	files, err := prFiles(repo, pr)
	if err != nil {
		return err
	}
	material := fmt.Sprintf("# PR #%d: %s\n%s\n\n# issue と受け入れ条件\n%s\n# CI\n%v\n\n# 差分\n```diff\n%s\n```\n",
		pr, v.Title, v.Body, issues.String(), checks, diff)

	// ロールごとに、会話を引き継がない別の Claude が自分の持ち場の目で見る (docs/ROLES.md)。
	// 途中で request_changes が出ても全員に見せる (直す側が 1 回で全部の指摘を受け取れるように)。
	result := reviews{Verdict: "approve"}
	for _, who := range reviewersFor(files) {
		instr, err := os.ReadFile(filepath.Join(e.cfg.Root, "harness", "reviewers", who+".md"))
		if err != nil {
			return err
		}
		out, err := runClaude(e, string(instr)+"\n\n"+material)
		if err != nil {
			return err
		}
		var r review
		if err := json.Unmarshal([]byte(extractJSON(out)), &r); err != nil || (r.Verdict != "approve" && r.Verdict != "request_changes") {
			return fmt.Errorf("%s の reviewer の出力が JSON の形でない: %v\n%s", who, err, out)
		}
		r.Role = who // 出力に role が書かれていても信じない
		comment := fmt.Sprintf("**wakuwaku reviewer (%s): %s**\n\n%s\n", who, r.Verdict, r.Summary)
		for _, is := range r.Issues {
			comment += "- " + is + "\n"
		}
		if _, err := gh.GH("pr", "comment", fmt.Sprint(pr), "--repo", repo, "--body", comment); err != nil {
			return err
		}
		if r.Verdict != "approve" {
			result.Verdict = "request_changes"
		}
		result.Reviews = append(result.Reviews, r)
	}
	// 全員が approve したときだけ、approve として記録する (wk merge はこの記録を見る)
	if result.Verdict == "approve" {
		if err := recordApproval(e, repo, pr, headSHA); err != nil {
			return err
		}
		if _, err := gh.GH("pr", "edit", fmt.Sprint(pr), "--repo", repo, "--add-label", "wk:reviewed"); err != nil {
			return err
		}
	} else {
		_, _ = gh.GH("pr", "edit", fmt.Sprint(pr), "--repo", repo, "--remove-label", "wk:reviewed")
	}
	return printJSON(result)
}

// runClaude は会話を引き継がない claude -p を、工場の外 (フックが効かない場所) で、道具なしで動かす。
func runClaude(e *env, prompt string) (string, error) {
	dir := filepath.Join(e.cfg.RuntimeDir, "claude-p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), claudeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "-p", "--no-session-persistence", "--tools", "", "--output-format", "text")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "OKUOS_JOURNAL=1")
	cmd.Stdin = strings.NewReader(prompt)
	cmd.WaitDelay = time.Second // 時間切れで止めたあと、子のプロセスが出力を握ったままでも待ち続けない
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude -p: %w", err)
	}
	return string(out), nil
}

// extractJSON は出力の中の最初の { から最後の } までを取り出す (```json で囲まれていても読めるように)。
func extractJSON(s string) string {
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j < i {
		return s
	}
	return s[i : j+1]
}

// ---- merge: CI 全部 green かつ reviewer の approve のときだけ ----

func cmdMerge(args []string) error {
	repo, pr, err := repoPR(args)
	if err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	if !e.pf.Repos()[repo] {
		return fmt.Errorf("%s は portfolio にない", repo)
	}
	labels, err := gh.Labels(repo, pr)
	if err != nil {
		return err
	}
	if !slices.Contains(labels, "wk:reviewed") {
		return errors.New("wk:reviewed がない (先に wk review で approve をもらう)")
	}
	// ラベルは誰でも付けられるので、wk review が記録した「approve した時点のコミット」と今の先頭を比べる
	if head, err := prHead(repo, pr); err != nil {
		return err
	} else if approved(e, repo, pr) != head {
		return errors.New("今のコミットは reviewer に approve されていない (push したあとは wk review をやり直す)")
	}
	if files, err := prFiles(repo, pr); err != nil {
		return err
	} else if hit := guard.TouchesProtected(files, appHard); len(hit) > 0 {
		return fmt.Errorf("工場の力を広げる守りの部分 (フック・検査の設定) を変えている PR は merge しない (人だけ): %s", strings.Join(hit, ", "))
	}
	start := time.Now()
	for {
		cs, err := gh.Checks(repo, pr)
		if err != nil {
			return err
		}
		pending, failed := 0, []string{}
		for _, c := range cs {
			switch c.Bucket {
			case "pending":
				pending++
			case "fail", "cancel":
				failed = append(failed, c.Name)
			}
		}
		// switch の case の条件はミューテーションの道具がカバレッジの範囲外と見なすので、if で書く
		if len(failed) > 0 {
			return fmt.Errorf("CI が赤: %s", strings.Join(failed, ", "))
		}
		if len(cs) == 0 && time.Since(start) > mergeNoCI {
			return errors.New("CI が 1 つも動いていない (雛形の workflow を確認)")
		}
		if len(cs) > 0 && pending == 0 {
			if _, err := gh.GH("pr", "merge", fmt.Sprint(pr), "--repo", repo, "--squash", "--delete-branch"); err != nil {
				return err
			}
			fmt.Printf("merge した: %s#%d (チェック %d 件 green)\n", repo, pr, len(cs))
			return nil
		}
		if time.Since(start) > mergeWait {
			return errors.New("CI が " + mergeWait.String() + " で終わらない")
		}
		time.Sleep(mergePoll)
	}
}

func repoPR(args []string) (string, int, error) {
	if len(args) != 2 {
		return "", 0, errors.New("使い方: <owner/repo> <pr 番号>")
	}
	var pr int
	if _, err := fmt.Sscan(args[1], &pr); err != nil {
		return "", 0, err
	}
	return args[0], pr, nil
}

// ---- フック ----

type hookInput struct {
	ToolName  string `json:"tool_name"`
	Cwd       string `json:"cwd"`
	ToolInput struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

func hookContext() (hookInput, guard.Context, error) {
	var in hookInput
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return in, guard.Context{}, err
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return in, guard.Context{}, err
	}
	if in.Cwd != "" {
		_ = os.Chdir(in.Cwd)
	}
	e, err := load()
	if err != nil {
		return in, guard.Context{}, err
	}
	root := e.cfg.Root
	protected := e.cfg.HardProtected() // 変えてよい守りの部分は、守りのレビューが後で見る (wk review・wk sync)
	// アプリ repo の中: 守るのは CI と フックの設定
	if top, err := gh.Run(in.Cwd, "git", "rev-parse", "--show-toplevel"); err == nil {
		if t := strings.TrimSpace(top); !strings.HasPrefix(t, e.cfg.Root) && t != e.cfg.MainCheckout && !isFactoryWorktree(t) {
			root, protected = t, appHard
		}
	}
	writable := []string{e.cfg.AppsDir, os.TempDir(), "/tmp"}
	return in, guard.Context{Owner: e.cfg.Owner, Factory: e.factoryRepo(), Owned: e.pf.Repos(), Root: root, Protected: protected, Writable: writable}, nil
}

func isFactoryWorktree(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "factory.json"))
	return err == nil
}

func deny(reason string) error {
	return exitError{2, "wakuwaku guard: " + reason}
}

func cmdGuardBash(args []string) error {
	in, c, err := hookContext()
	if err != nil {
		return deny("判定できないので止める: " + err.Error())
	}
	if why := guard.Bash(in.ToolInput.Command, c); why != "" {
		return deny(why)
	}
	return nil
}

func cmdGuardEdit(args []string) error {
	in, c, err := hookContext()
	if err != nil {
		return deny("判定できないので止める: " + err.Error())
	}
	if why := guard.Edit(in.ToolInput.FilePath, c); why != "" {
		return deny(why)
	}
	return nil
}

// reviewersFor は PR を通すのに approve が要るレビュアー。守りの部分に触れたら、最後に守りのレビュー (guardian) が入る。
func reviewersFor(files []string) []string {
	rs := role.Reviewers(files)
	if len(guard.TouchesProtected(files, appProtected)) > 0 {
		rs = append(rs, guardianReviewer)
	}
	return rs
}

// guardianReviewer は守りのレビュー (harness/reviewers/guardian.md)。守りを弱めない変更だけを通す。
const guardianReviewer = "guardian"

// appHard はアプリ repo の守りの部分のうち、工場が変えられないもの (フックの設定と、工場の検査が読む設定)。
var appHard = []string{".claude/", "wk.json"}

// appProtected はアプリ repo の守りの部分 (エージェントは変えられない。雛形から来たまま)。
// テストの基準 (カバレッジ 100%・ミューテーション・5 分) を書いたファイルも含む。
var appProtected = []string{".github/", ".claude/", "scripts/", "src/worker/security.ts", "wk.json", "wrangler.jsonc", ".npmrc",
	"Makefile", "vitest.config.ts", "stryker.config.json", "playwright.config.ts", "spec/README.md"}

func prFiles(repo string, pr int) ([]string, error) {
	out, err := gh.GH("pr", "view", fmt.Sprint(pr), "--repo", repo, "--json", "files", "--jq", ".files[].path")
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(out), "\n"), nil
}

func prHead(repo string, pr int) (string, error) {
	out, err := gh.GH("pr", "view", fmt.Sprint(pr), "--repo", repo, "--json", "headRefOid", "--jq", ".headRefOid")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(out)
	if len(sha) < 40 {
		return "", fmt.Errorf("PR の先頭のコミットを読めない: %q", sha)
	}
	return sha, nil
}

// approve の記録は runtime_dir に置く (guard でエージェントは触れない)。
func approvalsPath(e *env) string { return filepath.Join(e.cfg.RuntimeDir, "approvals.json") }

func loadApprovals(e *env) map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(approvalsPath(e)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func recordApproval(e *env, repo string, pr int, sha string) error {
	m := loadApprovals(e)
	m[fmt.Sprintf("%s#%d", repo, pr)] = sha
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(approvalsPath(e), b, 0o600)
}

func approved(e *env, repo string, pr int) string {
	return loadApprovals(e)[fmt.Sprintf("%s#%d", repo, pr)]
}
