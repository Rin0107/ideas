// Package gh は gh / git コマンドを呼ぶ薄い層。
package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
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
	out, err := GH("issue", "list", "--repo", repo, "--milestone", milestone, "--state", "open",
		"--limit", "100", "--json", "number,title,labels")
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

// CreateAndPush は private の repo を作り、dir の main を https で push する。
// 前の試みで repo だけができて push が失敗していた (空の repo がある) ときは、push だけやり直す。
func CreateAndPush(repo, dir, description string) error {
	if _, err := GH("repo", "view", repo, "--json", "name"); err != nil {
		if _, err := GH("repo", "create", repo, "--private", "--description", description); err != nil {
			return err
		}
	} else if _, err := GH("api", "repos/"+repo+"/commits?per_page=1", "--jq", ".[0].sha"); err == nil {
		return fmt.Errorf("%s はすでにあり、中身もある", repo)
	}
	if _, err := Run(dir, "git", "remote", "add", "origin", "https://github.com/"+repo+".git"); err != nil {
		return err
	}
	if err := SetupAuth(dir); err != nil {
		return err
	}
	_, err := Run(dir, "git", "push", "-q", "-u", "origin", "main")
	return err
}
