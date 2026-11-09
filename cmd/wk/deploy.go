package main

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/state"
)

// 鍵はエージェントに見せない。wk が ~/.config/wakuwaku/env を読み、子プロセスの環境変数にだけ渡す。
const secretsFile = "~/.config/wakuwaku/env"

func loadSecrets() (map[string]string, error) {
	p := config.Expand(secretsFile)
	st, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("%s がない (docs/SETUP.md)", secretsFile)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s の権限が広すぎる (chmod 600)", secretsFile)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		m[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return m, sc.Err()
}

// redact は出力から鍵の値を消す。
func redact(s string, secrets map[string]string) string {
	for _, v := range secrets {
		if len(v) >= 8 {
			s = strings.ReplaceAll(s, v, "***")
		}
	}
	return s
}

var workersURL = regexp.MustCompile(`https://[a-z0-9-]+\.[a-z0-9-]+\.workers\.dev`)

func cmdDeploy(args []string) error {
	e, err := load()
	if err != nil {
		return err
	}
	p, err := e.project()
	if err != nil {
		return err
	}
	dir := filepath.Join(e.cfg.AppsDir, filepath.Base(p.Repo))
	if _, err := gh.Run(dir, "git", "pull", "--ff-only", "origin", "main"); err != nil {
		return err
	}
	if br, _ := gh.Run(dir, "git", "branch", "--show-current"); strings.TrimSpace(br) != "main" {
		return errors.New(dir + " が main でない (デプロイは main の最新から)")
	}
	switch p.Stack {
	case "web":
		return deployWeb(e, p, dir)
	case "go-cli":
		return releaseGo(e, p, dir)
	}
	return fmt.Errorf("stack %q のデプロイ方法がない", p.Stack)
}

func deployWeb(e *env, p *state.Project, dir string) error {
	secrets, err := loadSecrets()
	if err != nil {
		return err
	}
	for _, k := range []string{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID"} {
		if secrets[k] == "" {
			return fmt.Errorf("%s に %s がない", secretsFile, k)
		}
	}
	envv := append(os.Environ(), "CLOUDFLARE_API_TOKEN="+secrets["CLOUDFLARE_API_TOKEN"],
		"CLOUDFLARE_ACCOUNT_ID="+secrets["CLOUDFLARE_ACCOUNT_ID"], "WRANGLER_SEND_METRICS=false")
	var out strings.Builder
	for _, c := range [][]string{{"npm", "ci", "--no-audit", "--no-fund"}, {"npm", "run", "verify"}, {"npx", "wrangler", "deploy"}} {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir, cmd.Env = dir, envv
		b, err := cmd.CombinedOutput()
		out.Write(b)
		if err != nil {
			return fmt.Errorf("%s が失敗:\n%s", strings.Join(c, " "), tail(redact(out.String(), secrets), 60))
		}
	}
	url := workersURL.FindString(out.String())
	if url == "" {
		return errors.New("デプロイ先の URL が出力にない:\n" + tail(redact(out.String(), secrets), 30))
	}
	if probs := smoke(url); len(probs) > 0 {
		return fmt.Errorf("%s の検査に通らない:\n  %s", url, strings.Join(probs, "\n  "))
	}
	if p.URL != url {
		p.URL = url
		e.event(p, "deployed", url)
		if err := e.save(); err != nil {
			return err
		}
	}
	fmt.Println("デプロイした:", url)
	return nil
}

// smoke は本番が動いていて、守りのヘッダーが付いているかを見る。
func smoke(url string) []string {
	c := http.Client{Timeout: 20 * time.Second}
	r, err := c.Get(url)
	if err != nil {
		return []string{err.Error()}
	}
	r.Body.Close()
	var probs []string
	if r.StatusCode != 200 {
		probs = append(probs, fmt.Sprintf("ステータス %d", r.StatusCode))
	}
	csp := r.Header.Get("Content-Security-Policy")
	switch {
	case csp == "":
		probs = append(probs, "Content-Security-Policy がない")
	case strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval"):
		probs = append(probs, "CSP に unsafe-inline / unsafe-eval がある")
	}
	for h, want := range map[string]string{
		"X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer",
		"Strict-Transport-Security": "max-age=", "Permissions-Policy": "camera=()",
	} {
		if !strings.Contains(r.Header.Get(h), want) {
			probs = append(probs, h+" に "+want+" がない")
		}
	}
	if r.Header.Get("Set-Cookie") != "" {
		probs = append(probs, "Cookie を発行している (持たない設計にする)")
	}
	return probs
}

// releaseGo はタグを打って push する。リリースは Actions の goreleaser が作る (鍵は GITHUB_TOKEN だけ)。
func releaseGo(e *env, p *state.Project, dir string) error {
	out, _ := gh.Run(dir, "git", "tag", "--list", "v*", "--sort=-v:refname")
	last := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	var maj, min, pat int
	if last != "" {
		if _, err := fmt.Sscanf(last, "v%d.%d.%d", &maj, &min, &pat); err != nil {
			return fmt.Errorf("タグ %q を読めない", last)
		}
	}
	tag := fmt.Sprintf("v0.%d.0", min+1)
	if _, err := gh.Run(dir, "git", "tag", "-a", tag, "-m", "release "+tag); err != nil {
		return err
	}
	if _, err := gh.Run(dir, "git", "push", "origin", tag); err != nil {
		return err
	}
	url := "https://github.com/" + p.Repo + "/releases/tag/" + tag
	p.URL = url
	e.event(p, "released", tag)
	if err := e.save(); err != nil {
		return err
	}
	fmt.Println("タグを push した (リリースは Actions が作る):", url)
	return nil
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
