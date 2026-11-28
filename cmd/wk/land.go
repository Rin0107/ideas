package main

// wk land: v4 の task を、PR を使わずに main に入れる (2026-11-28)。
// public の repo で PR を開くと、GitHub が試しの merge のコミット (refs/pull/N/merge) を持ち主の本名と会社のメールで作り、
// その ref は持ち主でも消せない (kisekae-closet で漏れた)。持ち主:「直しの提案なしでいい。毎日進捗チェックしてる」。
// そこで task のブランチ (wk/t<N>) をそのまま見る: wk verify が通った先頭か → 守りの部分に触れていないか →
// そのコミットの CI が全部 green か → reviewer 全員が approve か → main に早送りで push し、issue を閉じる。

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/guard"
	"github.com/Rin0107/ideas/internal/state"
)

func cmdLand(args []string) error {
	fs := flag.NewFlagSet("land", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	var num int
	if fs.NArg() != 1 {
		return errors.New("使い方: wk land <issue>")
	} else if _, err := fmt.Sscan(fs.Arg(0), &num); err != nil || num <= 0 {
		return fmt.Errorf("issue の番号が違う: %q", fs.Arg(0))
	}
	e, err := load()
	if err != nil {
		return err
	}
	a, _, err := readCurrent(e)
	if err != nil || a.Phase != state.PhaseTask || a.Issue != num {
		return fmt.Errorf("wk land は task の tick の中で、その task (#%d) だけに使う", num)
	}
	repo, branch := a.Repo, fmt.Sprintf("wk/t%d", num)
	dir := filepath.Join(e.cfg.AppsDir, filepath.Base(repo))
	if _, err := git(dir, "fetch", "-q", "origin", "main", branch); err != nil {
		return err
	}
	out, err := git(dir, "rev-parse", "origin/"+branch)
	if err != nil {
		return err
	}
	head := strings.TrimSpace(out)
	if loadVerified(e)[verifiedKey(repo, num)] != head {
		return fmt.Errorf("push した %s の先頭で wk verify が通っていない (push する前に wk verify)", branch)
	}
	if _, err := git(dir, "merge-base", "--is-ancestor", "origin/main", head); err != nil {
		return errors.New(branch + " が main の最新の上にない (git merge origin/main → wk verify → push からやり直す)")
	}
	names, err := git(dir, "diff", "--name-only", "origin/main..."+head)
	if err != nil {
		return err
	}
	files := strings.Fields(names)
	if hit := guard.TouchesProtected(files, appHard); len(hit) > 0 {
		return fmt.Errorf("工場の力を広げる守りの部分 (フック・検査の設定) を変えているので入れない (人だけ): %s", strings.Join(hit, ", "))
	}
	diff, err := git(dir, "diff", "origin/main..."+head)
	if err != nil {
		return err
	}
	if len(diff) > maxDiffBytes {
		return fmt.Errorf("差分が大きすぎる (%d バイト)。issue を分ける", len(diff))
	}
	if err := waitChecks(func() ([]gh.Check, error) { return gh.CommitChecks(repo, head) }); err != nil {
		return err
	}
	issue, err := gh.GH("issue", "view", fmt.Sprint(num), "--repo", repo, "--json", "number,title,body",
		"--jq", `"#\(.number) \(.title)\n\(.body)"`)
	if err != nil {
		return err
	}
	material := fmt.Sprintf("# task のブランチ %s\n\n# issue と受け入れ条件\n%s\n\n# CI\n全部 green\n\n# 差分\n```diff\n%s\n```\n",
		branch, issue, diff)
	result, err := reviewAll(e, files, material, func(comment string) error {
		_, err := gh.GH("issue", "comment", fmt.Sprint(num), "--repo", repo, "--body", comment)
		return err
	})
	if err != nil {
		return err
	}
	if result.Verdict != "approve" {
		return errors.Join(printJSON(result), errors.New("request_changes: 指摘を直して、コミット → wk verify → push → wk land"))
	}
	if _, err := git(dir, "push", "-q", "origin", head+":refs/heads/main"); err != nil {
		return err
	}
	// ここから先は main に入れたあとの片付け。落ちても入れたことは済んでいる (知らせるだけ)
	msg := fmt.Sprintf("wakuwaku factory: %s (%s) を main に入れた", branch, head[:8])
	if _, err := gh.GH("issue", "close", fmt.Sprint(num), "--repo", repo, "--comment", msg); err != nil {
		fmt.Fprintln(os.Stderr, "issue を閉じられない:", num, err)
	}
	if _, err := git(dir, "push", "-q", "origin", "--delete", branch); err != nil {
		fmt.Fprintln(os.Stderr, "ブランチを消せない:", branch, err)
	}
	fmt.Printf("main に入れた: %s %s (%s)\n", repo, branch, head[:8])
	return nil
}
