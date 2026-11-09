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
	// 鍵の通り道を 1 本にする:
	//  1. アプリのビルドと検査は鍵なしで動かす (アプリのスクリプトはエージェントが書いたものなので信用しない)
	//  2. install script は動かさない
	//  3. 鍵を渡すのは、wk が自分で入れた版固定の wrangler だけ。環境変数も最小にする
	//  4. wrangler の設定にビルドのコマンドがあれば止める (鍵のある環境で任意のコマンドが動くため)
	if err := checkWranglerConfig(dir); err != nil {
		return err
	}
	wrangler, err := ensureWrangler(e)
	if err != nil {
		return err
	}
	var out strings.Builder
	run := func(envv []string, name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = dir, envv
		b, err := cmd.CombinedOutput()
		out.Write(b)
		if err != nil {
			return fmt.Errorf("%s %s が失敗:\n%s", name, strings.Join(args, " "), tail(redact(out.String(), secrets), 60))
		}
		return nil
	}
	noSecrets := scrubEnv(os.Environ())
	if err := run(noSecrets, "npm", "ci", "--no-audit", "--no-fund", "--ignore-scripts"); err != nil {
		return err
	}
	if err := run(noSecrets, "npm", "run", "verify"); err != nil {
		return err
	}
	deployEnv := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "WRANGLER_SEND_METRICS=false", "CI=1",
		"CLOUDFLARE_API_TOKEN=" + secrets["CLOUDFLARE_API_TOKEN"], "CLOUDFLARE_ACCOUNT_ID=" + secrets["CLOUDFLARE_ACCOUNT_ID"]}
	if err := run(deployEnv, wrangler, "deploy", "--config", "wrangler.jsonc"); err != nil {
		return err
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

// wranglerVersion は wk が使う wrangler の版 (雛形の package.json と合わせる)。
const wranglerVersion = "4.158.0"

// ensureWrangler は runtime_dir/tools に版を固定した wrangler を入れ、その実行ファイルを返す。
// アプリの node_modules の wrangler は使わない (依存のすり替えで鍵を盗まれないように)。
func ensureWrangler(e *env) (string, error) {
	dir := filepath.Join(e.cfg.RuntimeDir, "tools", "wrangler-"+wranglerVersion)
	bin := filepath.Join(dir, "node_modules", ".bin", "wrangler")
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("npm", "install", "--no-audit", "--no-fund", "--ignore-scripts", "--prefix", dir, "wrangler@"+wranglerVersion)
	cmd.Env = scrubEnv(os.Environ())
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("wrangler を入れられない: %w\n%s", err, tail(string(b), 20))
	}
	return bin, nil
}

// checkWranglerConfig は、鍵のある環境で任意のコマンドが動く設定を止める。
func checkWranglerConfig(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, "wrangler.jsonc"))
	if err != nil {
		return fmt.Errorf("wrangler.jsonc がない: %w", err)
	}
	s := string(b)
	for _, bad := range []string{`"build"`, `"custom_build"`, `"routes"`, `"route"`, `"secrets"`, `"vars"`} {
		if strings.Contains(s, bad) {
			return fmt.Errorf("wrangler.jsonc に %s がある (使わない設定)", bad)
		}
	}
	for _, f := range []string{"wrangler.toml", "wrangler.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return fmt.Errorf("%s がある (設定は wrangler.jsonc の 1 つだけ)", f)
		}
	}
	return nil
}

// scrubEnv は鍵らしい環境変数を取り除く (gh のトークンなど、元から環境にあるものも含めて)。
func scrubEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		u := strings.ToUpper(k)
		if strings.Contains(u, "TOKEN") || strings.Contains(u, "SECRET") || strings.Contains(u, "KEY") ||
			strings.Contains(u, "PASSWORD") || strings.Contains(u, "CREDENTIAL") || strings.HasPrefix(u, "CLOUDFLARE_") ||
			strings.HasPrefix(u, "GH_") || strings.HasPrefix(u, "GITHUB_") || strings.HasPrefix(u, "ANTHROPIC_") || strings.HasPrefix(u, "AWS_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
