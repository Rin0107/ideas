// Package gh は gh / git コマンドを呼ぶ薄い層。
package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/Rin0107/ideas/internal/next"
)

// Run は外部コマンドを実行し、標準出力を返す。失敗時は標準エラーを含める。
func Run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func GH(args ...string) (string, error) { return Run("", "gh", args...) }

// OpenIssues はマイルストーンの open な issue。
func OpenIssues(repo, milestone string) ([]next.Issue, error) {
	return issues("issue", "list", "--repo", repo, "--milestone", milestone, "--state", "open",
		"--limit", "100", "--json", "number,title,labels")
}

// LabeledIssues はそのラベルの付いた open な issue (マイルストーンを問わない)。
func LabeledIssues(repo, label string) ([]next.Issue, error) {
	return issues("issue", "list", "--repo", repo, "--label", label, "--state", "open",
		"--limit", "100", "--json", "number,title,labels")
}

func issues(args ...string) ([]next.Issue, error) {
	out, err := GH(args...)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	is := make([]next.Issue, 0, len(raw))
	for _, r := range raw {
		i := next.Issue{Number: r.Number, Title: r.Title}
		for _, l := range r.Labels {
			i.Labels = append(i.Labels, l.Name)
		}
		is = append(is, i)
	}
	return is, nil
}

type Check struct {
	Name   string `json:"name"`
	Bucket string `json:"bucket"` // pass / fail / pending / skipping / cancel
}

// Checks は PR のチェック結果。
func Checks(repo string, pr int) ([]Check, error) {
	out, err := GH("pr", "checks", fmt.Sprint(pr), "--repo", repo, "--json", "name,bucket")
	if err != nil && out == "" {
		return nil, err
	}
	var cs []Check
	if err := json.Unmarshal([]byte(out), &cs); err != nil {
		return nil, fmt.Errorf("pr checks の出力を読めない: %w", err)
	}
	return cs, nil
}

// CommitChecks は、そのコミットの CI (check run) の結果。PR を使わない流れ (wk land) で、ブランチの先頭を見る。
func CommitChecks(repo, sha string) ([]Check, error) {
	out, err := GH("api", "repos/"+repo+"/commits/"+sha+"/check-runs?per_page=100",
		"--jq", "[.check_runs[] | {name, status, conclusion}]")
	if err != nil {
		return nil, err
	}
	var runs []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	if err := json.Unmarshal([]byte(out), &runs); err != nil {
		return nil, fmt.Errorf("check run の出力を読めない: %w", err)
	}
	cs := make([]Check, 0, len(runs))
	for _, r := range runs {
		cs = append(cs, Check{Name: r.Name, Bucket: bucket(r.Status, r.Conclusion)})
	}
	return cs, nil
}

// bucket は check run の状態を、gh pr checks と同じ分け方 (pass / fail / pending / skipping / cancel) にする。
func bucket(status, conclusion string) string {
	if status != "completed" {
		return "pending"
	}
	switch conclusion {
	case "success", "neutral":
		return "pass"
	case "skipped":
		return "skipping"
	case "cancelled":
		return "cancel"
	}
	return "fail" // failure・timed_out・action_required・startup_failure など
}

func Labels(repo string, pr int) ([]string, error) {
	out, err := GH("pr", "view", fmt.Sprint(pr), "--repo", repo, "--json", "labels", "--jq", "[.labels[].name]")
	if err != nil {
		return nil, err
	}
	var ls []string
	return ls, json.Unmarshal([]byte(out), &ls)
}

// 認証は gh に任せる (ssh の鍵やパスフレーズに頼らない。無人の定期実行で止まらないように)。
var authArgs = []string{"-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential"}

// SetupAuth は dir の repo で、push・pull の認証を gh に任せる設定をする。
func SetupAuth(dir string) error {
	for _, c := range [][]string{
		{"config", "--local", "--unset-all", "credential.helper"},
		{"config", "--local", "--add", "credential.helper", ""},
		{"config", "--local", "--add", "credential.helper", "!gh auth git-credential"},
	} {
		if _, err := Run(dir, "git", c...); err != nil && c[2] != "--unset-all" {
			return err
		}
	}
	return nil
}

// Clone は repo を https で dir に clone し、認証を gh に任せる。
func Clone(repo, dir string) error {
	args := append(append([]string{}, authArgs...), "clone", "-q", "https://github.com/"+repo+".git", dir)
	if _, err := Run("", "git", args...); err != nil {
		return err
	}
	return SetupAuth(dir)
}

// CloneMirror は repo の全部の ref (ブランチ・タグに加えて GitHub の refs/pull/*) を dir に bare で写す。
// public にする前の検査用。PR の ref は消したブランチのコミットを持ち続けるので、ブランチだけでは足りない。
func CloneMirror(repo, dir string) error {
	args := append(append([]string{}, authArgs...), "clone", "-q", "--mirror", "https://github.com/"+repo+".git", dir)
	_, err := Run("", "git", args...)
	return err
}

// CreateAndPush は repo を作り (public か private)、dir の main を https で push する。
// 工場のアプリは作っている間から public (2026-11-28)。配信用の repo は Cloudflare とつながるので private。
// 前の試みで repo だけができて push が失敗していた (空の repo がある) ときは、push だけやり直す。
func CreateAndPush(repo, dir, description string, public bool) error {
	vis := "--private"
	if public {
		vis = "--public"
	}
	if _, err := GH("repo", "view", repo, "--json", "name"); err != nil {
		if _, err := GH("repo", "create", repo, vis, "--description", description); err != nil {
			return err
		}
	} else if _, err := GH("api", "repos/"+repo+"/commits?per_page=1", "--jq", ".[0].sha"); err == nil {
		return fmt.Errorf("%s はすでにあり、中身もある", repo)
	}
	if err := SetupAuth(dir); err != nil {
		return err
	}
	if _, err := Run(dir, "git", "remote", "add", "origin", "https://github.com/"+repo+".git"); err != nil {
		return err
	}
	_, err := Run(dir, "git", "push", "-q", "-u", "origin", "main")
	return err
}

// OpenWithLabel は、そのラベルの付いた open な issue の番号 (小さい順)。
func OpenWithLabel(repo, label string) ([]int, error) {
	out, err := GH("issue", "list", "--repo", repo, "--label", label, "--state", "open", "--limit", "100", "--json", "number")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("issue list の出力を読めない: %w", err)
	}
	ns := make([]int, 0, len(raw))
	for _, r := range raw {
		ns = append(ns, r.Number)
	}
	slices.Sort(ns)
	return ns, nil
}
