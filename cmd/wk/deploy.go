package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/state"
)

// 配信の考え方 (docs/DESIGN.md §6・§7.4):
//   - 鍵はこのマシンに置かない。Web アプリは配信用の repo (factory.json の site.repo) の public/<名前>/ に置き、
//     Cloudflare の GitHub 連携 (Workers Builds) がそれを配る。鍵は Cloudflare の中にだけある
//   - アプリのビルドは手元で、鍵らしい環境変数を消してから動かす。Cloudflare 側にビルドのコマンドはなく、
//     そこで動くのは配信用 repo の版固定の wrangler だけ (エージェントの書いたコードは鍵のある所で動かない)
//   - wk deploy → preview ブランチ (プレビューの URL で評価する)、wk ship → main (本番)、wk drop → public/<名前>/ を消す

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
		return deployPreview(e, p, dir)
	case "go-cli":
		return releaseGo(e, p, dir)
	}
	return fmt.Errorf("stack %q のデプロイ方法がない", p.Stack)
}

// deployPreview はアプリをビルドし、配信用 repo の preview ブランチに置いて、プレビューの URL を確かめる。
func deployPreview(e *env, p *state.Project, dir string) error {
	s, err := openSite(e)
	if err != nil {
		return err
	}
	slug := filepath.Base(p.Repo)
	dist, err := buildApp(dir, slug)
	if err != nil {
		return err
	}
	want, err := os.ReadFile(filepath.Join(dist, "index.html"))
	if err != nil {
		return err
	}
	if err := s.git("checkout", "-q", "-B", previewBranch, "origin/main"); err != nil {
		return err
	}
	if err := s.putApp(slug, dist); err != nil {
		return err
	}
	sha, err := s.commitPush(previewBranch, "preview: "+slug, true)
	if err != nil {
		return err
	}
	pr, err := s.ensurePR()
	if err != nil {
		return err
	}
	if err := s.waitChecks(sha); err != nil {
		return err
	}
	var tried []string
	for _, base := range s.previewBases(pr) {
		url := base + "/" + slug + "/"
		probs := smoke(url, want)
		if len(probs) == 0 {
			if p.Preview != url {
				p.Preview = url
				e.event(p, "preview", url)
				if err := e.save(); err != nil {
					return err
				}
			}
			fmt.Println("プレビューに出した:", url)
			// L のテスト: 本物のプレビューをブラウザで e2e する (アプリの repo の Actions で。落ちたらデプロイも失敗)
			return previewE2E(p.Repo, url, previewE2EWait)
		}
		tried = append(tried, url+": "+strings.Join(probs, " / "))
	}
	return errors.New("プレビューが確かめられない:\n  " + strings.Join(tried, "\n  "))
}

// publishWeb はプレビューで評価したものを本番 (配信用 repo の main) に出し、本番の URL を返す。
func publishWeb(e *env, p *state.Project) (string, error) {
	s, err := openSite(e)
	if err != nil {
		return "", err
	}
	slug := filepath.Base(p.Repo)
	if err := s.git("fetch", "-q", "origin", previewBranch); err != nil {
		return "", fmt.Errorf("preview ブランチがない (先に wk deploy): %w", err)
	}
	want, err := s.out("show", "origin/"+previewBranch+":public/"+slug+"/index.html")
	if err != nil {
		return "", fmt.Errorf("preview に %s がない (先に wk deploy): %w", slug, err)
	}
	if err := s.git("rm", "-r", "-q", "--ignore-unmatch", "public/"+slug); err != nil {
		return "", err
	}
	if err := s.git("checkout", "origin/"+previewBranch, "--", "public/"+slug); err != nil {
		return "", err
	}
	if err := s.writeIndex(e, slug, true); err != nil {
		return "", err
	}
	sha, err := s.commitPush("main", "ship: "+slug, false)
	if err != nil {
		return "", err
	}
	if err := s.waitChecks(sha); err != nil {
		return "", err
	}
	url := strings.TrimRight(e.cfg.Site.URL, "/") + "/" + slug + "/"
	if probs := smoke(url, []byte(want)); len(probs) > 0 {
		return "", fmt.Errorf("%s の検査に通らない:\n  %s", url, strings.Join(probs, "\n  "))
	}
	s.closePR()
	return url, nil
}

// unpublishWeb は本番からアプリを消す (drop のとき)。置いていなければ何もしない。
func unpublishWeb(e *env, p *state.Project) error {
	if e.cfg.Site.Repo == "" || e.cfg.Site.URL == "" { // 配信の準備前なら、出したものはない
		return nil
	}
	s, err := openSite(e)
	if err != nil {
		return err
	}
	defer s.closePR()
	slug := filepath.Base(p.Repo)
	if _, err := os.Stat(filepath.Join(s.dir, "public", slug)); err != nil {
		return nil
	}
	if err := s.git("rm", "-r", "-q", "public/"+slug); err != nil {
		return err
	}
	if err := s.writeIndex(e, slug, false); err != nil {
		return err
	}
	_, err = s.commitPush("main", "drop: "+slug, false)
	return err
}

// ---- アプリのビルド (鍵なし) ----

// buildApp は鍵らしい環境変数を消した環境で、install script なしで検査とビルドをし、dist のパスを返す。
func buildApp(dir, slug string) (string, error) {
	envv := scrubEnv(os.Environ())
	for _, c := range [][]string{
		{"npm", "ci", "--no-audit", "--no-fund", "--ignore-scripts"},
		{"npm", "run", "verify"},
		{"npm", "run", "build", "--", "--base=/" + slug + "/"},
	} {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir, cmd.Env = dir, envv
		if b, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("%s が失敗:\n%s", strings.Join(c, " "), tail(string(b), 60))
		}
	}
	return filepath.Join(dir, "dist"), nil
}

// 配るファイルの上限 (Workers の静的ファイルの無料枠より十分小さく)
const (
	maxDistFiles = 1000
	maxFileBytes = 10 << 20
	maxDistBytes = 50 << 20
)

// copyDist は dist を dst に写す。配信の設定を変えるファイル・リンク・大きすぎるものは止める。
func copyDist(src, dst string) error {
	if _, err := os.Stat(filepath.Join(src, "index.html")); err != nil {
		return errors.New("dist/index.html がない")
	}
	var files int
	var total int64
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		base := info.Name()
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("dist/%s はリンク (置けない)", rel)
		case base == "_headers" || base == "_redirects" || base == "_routes.json" || base == ".assetsignore" ||
			strings.HasPrefix(base, "_worker") || base == "sw.js" || base == "service-worker.js":
			return fmt.Errorf("dist/%s は配信の設定・Service Worker (置けない)", rel)
		case info.IsDir():
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		case !info.Mode().IsRegular():
			return fmt.Errorf("dist/%s は普通のファイルでない", rel)
		}
		files++
		total += info.Size()
		if files > maxDistFiles || info.Size() > maxFileBytes || total > maxDistBytes {
			return fmt.Errorf("dist が大きすぎる (%d ファイル・%d バイトまで)", maxDistFiles, maxDistBytes)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
}

// ---- 本番・プレビューの検査 ----

// smoke は URL が動いていて、守りのヘッダーが付き、中身が want (いま置いた index.html) と同じかを見る。
func smoke(url string, want []byte) []string {
	c := http.Client{Timeout: 20 * time.Second, Transport: httpTransport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "text/html")
	r, err := c.Do(req)
	if err != nil {
		return []string{err.Error()}
	}
	defer r.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
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
	for h, w := range map[string]string{
		"X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer",
		"Strict-Transport-Security": "max-age=", "Permissions-Policy": "camera=()",
	} {
		if !strings.Contains(r.Header.Get(h), w) {
			probs = append(probs, h+" に "+w+" がない")
		}
	}
	if r.Header.Get("Set-Cookie") != "" {
		probs = append(probs, "Cookie を発行している (持たない設計にする)")
	}
	if want != nil && r.StatusCode == 200 && !bytes.Equal(bytes.TrimSpace(body), bytes.TrimSpace(want)) {
		probs = append(probs, "中身がいま置いたものと違う (古い版が出ている)")
	}
	return probs
}

// ---- Go の CLI ----

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

// previewE2EWait は L のテストの結果を待つ長さ (ジョブは 5 分で打ち切られる)。
var (
	previewE2EWait = 10 * time.Minute
	previewPoll    = 15 * time.Second
)

// httpTransport は本番・プレビューの検査で使う (テストでは手元のサーバーに向ける)。
var httpTransport http.RoundTripper = http.DefaultTransport

// previewE2E はアプリの repo の preview-e2e.yml を起動し、終わるのを待つ。
func previewE2E(repo, url string, wait time.Duration) error {
	since := time.Now().UTC().Add(-time.Minute)
	if _, err := gh.GH("workflow", "run", "preview-e2e.yml", "--repo", repo, "--ref", "main", "-f", "base_url="+url); err != nil {
		return fmt.Errorf("プレビューの e2e を起動できない: %w", err)
	}
	deadline := time.Now().Add(wait)
	for {
		out, err := gh.GH("run", "list", "--repo", repo, "--workflow", "preview-e2e.yml", "--event", "workflow_dispatch",
			"--limit", "5", "--json", "createdAt,status,conclusion,url")
		if err == nil {
			if r, ok := latestRun(out, since); ok && r.Status == "completed" {
				if r.Conclusion != "success" {
					return fmt.Errorf("プレビューの e2e が %s: %s", r.Conclusion, r.URL)
				}
				fmt.Println("プレビューの e2e が通った:", r.URL)
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("プレビューの e2e が終わらない (" + wait.String() + ")")
		}
		time.Sleep(previewPoll)
	}
}

type runInfo struct {
	CreatedAt  time.Time `json:"createdAt"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	URL        string    `json:"url"`
}

// latestRun は since より後に作られた、いちばん新しい実行を返す。
func latestRun(out string, since time.Time) (runInfo, bool) {
	var rs []runInfo
	if json.Unmarshal([]byte(out), &rs) != nil {
		return runInfo{}, false
	}
	var best runInfo
	found := false
	for _, r := range rs {
		if !r.CreatedAt.Before(since) && (!found || r.CreatedAt.After(best.CreatedAt)) {
			best, found = r, true
		}
	}
	return best, found
}
