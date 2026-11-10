package main

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/state"
)

// 配信用 repo (factory.json の site) の操作。手元の写しは runtime_dir/site にあり、wk だけが触る
// (エージェントは guard で runtime_dir に入れず、gh での書き込みも portfolio の repo に限られる)。

const previewBranch = "preview"

type site struct {
	repo, url, dir string
	owner          string
}

func openSite(e *env) (*site, error) {
	if e.cfg.Site.Repo == "" || e.cfg.Site.URL == "" {
		return nil, errors.New("配信の準備がまだ (factory.json の site.repo・site.url。docs/SETUP.md)")
	}
	s := &site{repo: e.cfg.Site.Repo, url: strings.TrimRight(e.cfg.Site.URL, "/"),
		dir: filepath.Join(e.cfg.RuntimeDir, "site"), owner: e.cfg.Owner}
	if _, err := os.Stat(filepath.Join(s.dir, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(s.dir), 0o755); err != nil {
			return nil, err
		}
		if _, err := gh.GH("repo", "clone", s.repo, s.dir, "--", "-q"); err != nil {
			return nil, err
		}
	}
	for _, c := range [][]string{{"fetch", "-q", "--prune", "origin"}, {"checkout", "-q", "-f", "-B", "main", "origin/main"}, {"clean", "-q", "-f", "-d", "-x"}} {
		if err := s.git(c...); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *site) git(args ...string) error {
	_, err := gh.Run(s.dir, "git", args...)
	return err
}

func (s *site) out(args ...string) (string, error) { return gh.Run(s.dir, "git", args...) }

// putApp は public/<slug>/ を dist で置き換える。
func (s *site) putApp(slug, dist string) error {
	dst := filepath.Join(s.dir, "public", slug)
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return copyDist(dist, dst)
}

// commitPush は public/ の変更をコミットして push し、その SHA を返す。変更がなくてもコミットする (配信をやり直すため)。
func (s *site) commitPush(branch, msg string, force bool) (string, error) {
	if err := s.git("add", "-A", "public"); err != nil {
		return "", err
	}
	if err := s.git("-c", "user.name=wakuwaku factory", "-c", "user.email="+s.owner+"@users.noreply.github.com",
		"commit", "-q", "--allow-empty", "-m", msg); err != nil {
		return "", err
	}
	push := []string{"push", "-q"}
	if force {
		push = append(push, "--force")
	}
	if err := s.git(append(push, "origin", "HEAD:refs/heads/"+branch)...); err != nil {
		return "", err
	}
	sha, err := s.out("rev-parse", "HEAD")
	return strings.TrimSpace(sha), err
}

// ensurePR は preview → main の PR (Cloudflare がプレビューの URL を書く場所) の番号を返す。
func (s *site) ensurePR() (string, error) {
	out, err := gh.GH("pr", "list", "--repo", s.repo, "--head", previewBranch, "--state", "open", "--json", "number", "--jq", ".[0].number // empty")
	if err != nil {
		return "", err
	}
	if n := strings.TrimSpace(out); n != "" {
		return n, nil
	}
	out, err = gh.GH("pr", "create", "--repo", s.repo, "--head", previewBranch, "--base", "main", "--draft",
		"--title", "preview", "--body", "wk deploy が置いたプレビュー。評価が ship なら wk ship が main に出し、この PR は閉じる。")
	if err != nil {
		return "", err
	}
	u := strings.TrimSpace(out)
	return u[strings.LastIndex(u, "/")+1:], nil
}

func (s *site) closePR() {
	gh.GH("pr", "close", previewBranch, "--repo", s.repo) //nolint:errcheck // 閉じる PR がなくてもよい
}

// waitChecks は Cloudflare のビルド (GitHub の check run) が終わるのを待つ。
func (s *site) waitChecks(sha string) error {
	start := time.Now()
	for {
		out, err := gh.GH("api", "repos/"+s.repo+"/commits/"+sha+"/check-runs", "--jq",
			`.check_runs[] | .name + "\t" + .status + "\t" + (.conclusion // "")`)
		if err == nil {
			done, ok, lines := checkRunsState(out)
			if done && ok {
				return nil
			}
			if done {
				return errors.New("Cloudflare のビルドが失敗:\n  " + strings.Join(lines, "\n  "))
			}
			if len(lines) == 0 && time.Since(start) > 5*time.Minute {
				return errors.New("check run が出ない (Cloudflare と " + s.repo + " の連携を確かめる。docs/SETUP.md)")
			}
		}
		if time.Since(start) > 20*time.Minute {
			return errors.New("Cloudflare のビルドが 20 分で終わらない")
		}
		time.Sleep(20 * time.Second)
	}
}

// checkRunsState は check run の一覧 (名前\t状態\t結論) から、全部終わったか・全部通ったかを返す。
func checkRunsState(out string) (done, ok bool, lines []string) {
	done, ok = true, true
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l == "" {
			continue
		}
		lines = append(lines, l)
		f := strings.Split(l, "\t")
		if len(f) < 3 || f[1] != "completed" {
			done = false
			continue
		}
		if f[2] != "success" && f[2] != "neutral" && f[2] != "skipped" {
			ok = false
		}
	}
	if len(lines) == 0 {
		return false, false, nil
	}
	return done, ok, lines
}

var workersDev = regexp.MustCompile(`https://[a-z0-9-]+\.[a-z0-9-]+\.workers\.dev`)

// previewBases はプレビューの URL の候補 (新しい順)。Cloudflare が PR に書いた URL のうち、
// 自分の workers.dev の下のものだけを使う。最後に、ブランチ名から作った URL を足す。
func (s *site) previewBases(pr string) []string {
	var text string
	if out, err := gh.GH("api", "repos/"+s.repo+"/issues/"+pr+"/comments", "--jq", ".[].body"); err == nil {
		text = out
	}
	return previewCandidates(s.url, text)
}

func previewCandidates(siteURL, text string) []string {
	u, err := url.Parse(siteURL)
	if err != nil {
		return nil
	}
	worker, sub, _ := strings.Cut(u.Host, ".") // wakuwaku-site / <sub>.workers.dev
	found := workersDev.FindAllString(text, -1)
	slices.Reverse(found)
	var out []string
	for _, f := range append(found, "https://"+previewBranch+"-"+worker+"."+sub) {
		h := strings.TrimPrefix(f, "https://")
		if strings.HasSuffix(h, "-"+worker+"."+sub) && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// writeIndex は public/index.html (公開したアプリの一覧) を書く。add なら slug を足し、そうでなければ除く。
func (s *site) writeIndex(e *env, slug string, add bool) error {
	names := shippedWeb(e.pf)
	names = slices.DeleteFunc(names, func(n string) bool { return n == slug })
	if add {
		names = append(names, slug)
	}
	slices.Sort(names)
	return os.WriteFile(filepath.Join(s.dir, "public", "index.html"), renderIndex(names), 0o644)
}

func shippedWeb(pf *state.Portfolio) []string {
	var out []string
	for _, p := range pf.Projects {
		if p.Status == state.Shipped && p.Stack == "web" {
			out = append(out, filepath.Base(p.Repo))
		}
	}
	return out
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func renderIndex(names []string) []byte {
	var b strings.Builder
	b.WriteString(`<!doctype html>
<html lang="ja">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>wakuwaku</title>
<link rel="stylesheet" href="/site.css">
</head>
<body>
<h1>wakuwaku</h1>
<p>わくわく工場が作ったアプリ。どれもアカウントなしで、データは端末の中だけに置きます。</p>
<ul>
`)
	for _, n := range names {
		if !slugRe.MatchString(n) {
			continue
		}
		fmt.Fprintf(&b, "<li><a href=\"/%s/\">%s</a></li>\n", n, html.EscapeString(n))
	}
	if len(names) == 0 {
		b.WriteString("<li><small>まだありません</small></li>\n")
	}
	b.WriteString("</ul>\n</body>\n</html>\n")
	return []byte(b.String())
}

// ---- wk site init (人が 1 回だけ) ----

func cmdSite(args []string) error {
	if len(args) != 1 || args[0] != "init" {
		return errors.New("使い方: wk site init")
	}
	e, err := load()
	if err != nil {
		return err
	}
	repo := e.cfg.Site.Repo
	if repo == "" {
		return errors.New("factory.json の site.repo が空")
	}
	if _, err := gh.GH("repo", "view", repo, "--json", "name"); err == nil {
		return fmt.Errorf("%s はすでにある", repo)
	}
	tmp, err := os.MkdirTemp("", "wakuwaku-site-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	dir := filepath.Join(tmp, "site")
	if err := copyTemplate(filepath.Join(e.cfg.Root, "templates", "site"), dir, nil); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "public", "index.html"), renderIndex(shippedWeb(e.pf)), 0o644); err != nil {
		return err
	}
	for _, c := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "配信用の repo を作る (wakuwaku factory)"}} {
		if _, err := gh.Run(dir, "git", c...); err != nil {
			return err
		}
	}
	if _, err := gh.GH("repo", "create", repo, "--private", "--source", dir, "--push",
		"--description", "わくわく工場が公開したアプリを配る (Cloudflare Workers)"); err != nil {
		return err
	}
	fmt.Printf("作った: https://github.com/%s\n次: Cloudflare でこの repo を連携し、出た URL を factory.json の site.url に書く (docs/SETUP.md)\n", repo)
	return nil
}
