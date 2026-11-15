package main

// 配信 (deploy・ship・drop・site init) のテスト。
// 配信用の repo は手元の bare repo、Cloudflare は「push された中身をそのまま返す手元の TLS サーバー」、
// npm は偽物、gh は fakegh で置き換え、本物と同じ順番で全部の道を通す。

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/state"
)

// failGit は、引数が正規表現 re に合う git の呼び出しだけを失敗させる (それ以外は本物の git)。
func failGit(t *testing.T, re string) {
	t.Helper()
	real, err := exec.LookPath("git")
	must(t, err)
	d := t.TempDir()
	sh := "#!/bin/sh\nif printf '%s' \"$*\" | grep -Eq '" + re + "'; then echo \"fake git: 失敗 $*\" >&2; exit 1; fi\nexec " + real + " \"$@\"\n"
	must(t, os.WriteFile(filepath.Join(d, "git"), []byte(sh), 0o755))
	t.Setenv("PATH", d+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// fakeNpm は偽物の npm。build で dist/index.html (中身 html) を作る。fail に合う引数なら失敗する。shell は build のときに動かす。
func fakeNpm(t *testing.T, html, fail, shell string) {
	t.Helper()
	d := t.TempDir()
	sh := fmt.Sprintf(`#!/bin/sh
if [ -n %q ] && printf '%%s' "$*" | grep -Eq %q; then echo "npm: 失敗" >&2; exit 1; fi
case "$*" in
  *build*) mkdir -p dist && printf '%%s' %q > dist/index.html; %s ;;
esac
exit 0
`, fail, fail, html, shell)
	if html == "" {
		sh = strings.Replace(sh, "mkdir -p dist && printf", "true || printf", 1)
	}
	must(t, os.WriteFile(filepath.Join(d, "npm"), []byte(sh), 0o755))
	t.Setenv("PATH", d+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// cloudflare は配信用の repo の中身を返す手元のサーバー。preview-… などのホストは preview ブランチ、本番のホストは main。
func cloudflare(t *testing.T, siteGit string, headers bool) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		branch, gitDir := "preview", siteGit
		if strings.HasPrefix(r.Host, "wakuwaku-site.") {
			branch = "main"
		}
		if strings.HasPrefix(r.Host, "alpha.") { // web-db のアプリの Worker は、アプリの repo の deploy ブランチ
			branch, gitDir = "deploy", filepath.Join(filepath.Dir(siteGit), "alpha.git")
		}
		out, err := exec.Command("git", "--git-dir", gitDir, "show", branch+":public"+r.URL.Path+"index.html").Output()
		if headers {
			w.Header().Set("Content-Security-Policy", "default-src 'self'")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Strict-Transport-Security", "max-age=1")
			w.Header().Set("Permissions-Policy", "camera=()")
		}
		if err != nil {
			w.WriteHeader(404)
			return
		}
		w.Write(out)
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	old := httpTransport
	httpTransport = &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // テスト用の手元のサーバー
	}
	t.Cleanup(func() { httpTransport = old })
}

func fastChecks(t *testing.T) {
	t.Helper()
	p, n, w, pp := checkPoll, checkNoRun, checkWait, previewPoll
	checkPoll, checkNoRun, checkWait, previewPoll = time.Millisecond, time.Minute, time.Minute, time.Millisecond
	t.Cleanup(func() { checkPoll, checkNoRun, checkWait, previewPoll = p, n, w, pp })
}

type delivery struct {
	*factory
	siteGit, app string
}

// deliveryRules は配信の通り道の gh の決まり (Cloudflare のビルドは成功・プレビューの e2e も成功)。
func deliveryRules() []fakegh.Rule {
	now := time.Now().UTC().Format(time.RFC3339)
	return []fakegh.Rule{
		{Match: `^pr list --repo Tester/wakuwaku-site --head preview`, Out: ""},
		{Match: `^pr create --repo Tester/wakuwaku-site`, Out: "https://github.com/Tester/wakuwaku-site/pull/7\n"},
		{Match: `^api repos/Tester/wakuwaku-site/commits/[0-9a-f]+/check-runs`, Out: "Workers Builds: wakuwaku-site\tcompleted\tsuccess\n"},
		{Match: `^api repos/Tester/wakuwaku-site/issues/7/comments`, Out: "Preview: https://0123abcd-wakuwaku-site.tester.workers.dev\n"},
		{Match: `^workflow run preview-e2e.yml --repo Tester/alpha`},
		{Match: `^run list --repo Tester/alpha --workflow preview-e2e.yml`, Out: `[{"createdAt":"` + now + `","status":"completed","conclusion":"success","url":"https://run/1"}]`},
		{Match: `^pr close preview --repo Tester/wakuwaku-site`},
	}
}

// newDelivery は配信用の repo・アプリの repo・Cloudflare の代わり・偽物の npm をそろえる。
func newDelivery(t *testing.T, rules ...fakegh.Rule) *delivery {
	t.Helper()
	fastChecks(t)
	f := newFactory(t, append(rules, deliveryRules()...)...)
	d := &delivery{factory: f, siteGit: filepath.Join(f.remote, "Tester", "wakuwaku-site.git"), app: filepath.Join(f.apps, "alpha")}
	git := func(dir string, args ...string) {
		t.Helper()
		if out, err := runGit(dir, args...); err != nil {
			t.Fatal(out, err)
		}
	}
	// 配信用の repo (main に一覧のページだけ)
	seed := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(seed, "public"), 0o755))
	must(t, os.WriteFile(filepath.Join(seed, "public", "index.html"), renderIndex(nil), 0o644))
	git(seed, "init", "-q", "-b", "main")
	git(seed, "add", "-A")
	git(seed, "commit", "-q", "-m", "site")
	must(t, os.MkdirAll(filepath.Dir(d.siteGit), 0o755))
	git(filepath.Dir(d.siteGit), "clone", "-q", "--bare", seed, d.siteGit)
	// アプリの repo
	must(t, os.MkdirAll(d.app, 0o755))
	git(d.app, "init", "-q", "-b", "main")
	git(d.app, "commit", "-q", "--allow-empty", "-m", "app")
	git(filepath.Dir(d.siteGit), "clone", "-q", "--bare", d.app, "alpha.git")
	git(d.app, "remote", "add", "origin", "https://github.com/Tester/alpha.git")
	git(d.app, "fetch", "-q", "origin")
	git(d.app, "branch", "-q", "--set-upstream-to=origin/main", "main")
	f.projects(active())
	fakeNpm(t, "<p>alpha v1</p>", "", "")
	cloudflare(t, d.siteGit, true)
	return d
}

func (d *delivery) siteFile(branch, path string) string {
	out, _ := runGit(d.siteGit, "show", branch+":"+path)
	return out
}

// ---- deploy (Web: プレビュー) → ship → drop の通し ----

func TestDeployShipDropWeb(t *testing.T) {
	d := newDelivery(t,
		fakegh.Rule{Match: `^api repos/Tester/alpha/readme`, Out: "https://wakuwaku-site.tester.workers.dev/alpha/"},
		fakegh.Rule{Match: `^api repos/Tester/alpha/contents/`, Out: "x"},
		fakegh.Rule{Match: `^repo view Tester/alpha --json description`, Out: "3 2"},
		fakegh.Rule{Match: `^run list --repo Tester/alpha --branch main`, Out: "success"},
	)
	out, err := capture(t, func() error { return cmdDeploy(nil) })
	if err != nil || !strings.Contains(out, "プレビューに出した") || !strings.Contains(out, "e2e が通った") {
		t.Fatal(out, err)
	}
	p := d.portfolio().Active()
	if p.Preview != "https://0123abcd-wakuwaku-site.tester.workers.dev/alpha/" {
		t.Fatal("PR のコメントの URL を使っていない:", p.Preview)
	}
	if got := d.siteFile("preview", "public/alpha/index.html"); got != "<p>alpha v1</p>" {
		t.Fatal(got)
	}
	// もう一度出しても (同じ URL)、記録は増えない
	n := len(p.History)
	_, err = capture(t, func() error { return cmdDeploy(nil) })
	must(t, err)
	if len(d.portfolio().Active().History) != n {
		t.Fatal("同じプレビューを二重に記録した")
	}

	// ship: プレビューと同じものを main に
	d.projects(func() *state.Project {
		p := d.portfolio().Active()
		p.Status, p.Verdict = state.Evaluating, "ship"
		return p
	}())
	must(t, cmdShip(nil))
	if got := d.siteFile("main", "public/alpha/index.html"); got != "<p>alpha v1</p>" {
		t.Fatal("main に出ていない:", got)
	}
	if !strings.Contains(d.siteFile("main", "public/index.html"), `href="/alpha/"`) {
		t.Fatal("一覧に載っていない")
	}
	if q := d.portfolio().Projects[0]; q.Status != state.Shipped || q.URL != "https://wakuwaku-site.tester.workers.dev/alpha/" || q.Preview != "" {
		t.Fatalf("%+v", q)
	}

	// drop (公開したあと捨てる): 本番から消える
	d.projects(func() *state.Project { p := d.portfolio().Projects[0]; p.Status = state.Iterating; return p }())
	must(t, cmdDrop([]string{"--reason", "使われないので捨てて次に行く"}))
	if d.siteFile("main", "public/alpha/index.html") != "" || strings.Contains(d.siteFile("main", "public/index.html"), "alpha") {
		t.Fatal("本番から消えていない")
	}
	// 置いていないものを捨てるときは何もしない
	d.projects(active(func(p *state.Project) { p.Repo = "Tester/never-shipped" }))
	must(t, cmdDrop([]string{"--reason", "作る前に捨てると決めた"}))
}

func TestDeployErrors(t *testing.T) {
	cases := map[string]struct {
		rules []fakegh.Rule
		setup func(d *delivery)
		want  string
	}{
		"設定が壊れている":            {setup: func(d *delivery) { d.write("factory.json", "{") }, want: "factory.json"},
		"作っているものがない":          {setup: func(d *delivery) { d.projects() }, want: "作っているアプリがない"},
		"pull できない":           {setup: func(d *delivery) { failGit(d.t, "^pull") }, want: "pull"},
		"main でない":            {setup: func(d *delivery) { _, _ = runGit(d.app, "switch", "-q", "-c", "feature") }, want: "main でない"},
		"知らない stack":          {setup: func(d *delivery) { d.projects(active(func(p *state.Project) { p.Stack = "ios" })) }, want: "ios"},
		"配信の準備がまだ":            {setup: func(d *delivery) { d.writeConfig(map[string]any{"site": map[string]string{"repo": "x/y", "url": ""}}) }, want: "配信の準備"},
		"配信用の repo がない":       {setup: func(d *delivery) { os.RemoveAll(d.siteGit) }, want: "clone"},
		"site の置き場所を作れない":     {setup: func(d *delivery) { os.RemoveAll(d.runtime); d.write(d.runtime, "") }, want: "runtime"},
		"site を最新にできない":       {setup: func(d *delivery) { failGit(d.t, "^fetch -q --prune") }, want: "fetch"},
		"ビルドが失敗":              {setup: func(d *delivery) { fakeNpm(d.t, "x", "verify", "") }, want: "npm run verify が失敗"},
		"index.html ができない":    {setup: func(d *delivery) { fakeNpm(d.t, "", "", "") }, want: "index.html"},
		"preview を作れない":       {setup: func(d *delivery) { failGit(d.t, "^checkout -q -B preview") }, want: "checkout"},
		"dist を置けない":          {setup: func(d *delivery) { fakeNpm(d.t, "x", "", "touch dist/_headers") }, want: "_headers"},
		"add できない":            {setup: func(d *delivery) { failGit(d.t, "^add -A public") }, want: "add"},
		"commit できない":         {setup: func(d *delivery) { failGit(d.t, "commit -q --allow-empty") }, want: "commit"},
		"push できない":           {setup: func(d *delivery) { failGit(d.t, "^push") }, want: "push"},
		"SHA が読めない":           {setup: func(d *delivery) { failGit(d.t, "^rev-parse HEAD") }, want: "rev-parse"},
		"PR が読めない":            {rules: []fakegh.Rule{{Match: `^pr list`, Code: 1}}, want: "pr list"},
		"PR を作れない":            {rules: []fakegh.Rule{{Match: `^pr create`, Code: 1}}, want: "pr create"},
		"ビルドが失敗 (Cloudflare)": {rules: []fakegh.Rule{{Match: `check-runs`, Out: "Workers Builds\tcompleted\tfailure\n"}}, want: "Cloudflare のビルドが失敗"},
		"check run が出ない":      {rules: []fakegh.Rule{{Match: `check-runs`, Out: ""}}, setup: func(*delivery) { checkNoRun = 0 }, want: "check run が出ない"},
		"ビルドが終わらない":           {rules: []fakegh.Rule{{Match: `check-runs`, Out: "W\tin_progress\t\n"}}, setup: func(*delivery) { checkWait = 0 }, want: "終わらない"},
		"プレビューに守りのヘッダーがない":    {setup: func(d *delivery) { cloudflare(d.t, d.siteGit, false) }, want: "確かめられない"},
		"portfolio が書けない":     {setup: func(d *delivery) { d.block("state/portfolio.json.tmp") }, want: "portfolio.json"},
		"e2e を起動できない":         {rules: []fakegh.Rule{{Match: `^workflow run`, Code: 1}}, want: "起動できない"},
		"e2e が落ちる": {rules: []fakegh.Rule{{Match: `^run list --repo Tester/alpha --workflow`,
			Out: `[{"createdAt":"` + time.Now().UTC().Format(time.RFC3339) + `","status":"completed","conclusion":"failure","url":"https://run/2"}]`}}, want: "failure: https://run/2"},
		"e2e が終わらない": {rules: []fakegh.Rule{{Match: `^run list --repo Tester/alpha --workflow`, Code: 1}},
			setup: func(*delivery) { previewE2EWait = 0 }, want: "終わらない"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d := newDelivery(t, c.rules...)
			old := previewE2EWait
			t.Cleanup(func() { previewE2EWait = old })
			if c.setup != nil {
				c.setup(d)
			}
			_, err := capture(t, func() error { return cmdDeploy(nil) })
			errHas(t, err, c.want)
		})
	}
}

func TestDeployPreviewWaitsForBuildAndE2E(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	d := newDelivery(t,
		fakegh.Rule{Match: `check-runs`, Out: "W\tqueued\t\n", Times: 1},
		fakegh.Rule{Match: `check-runs`, Code: 1, Times: 1},                                   // 一時的な失敗は待ち続ける
		fakegh.Rule{Match: `^api repos/Tester/wakuwaku-site/issues/7/comments`, Code: 1},      // コメントが読めなくてもブランチ名の URL がある
		fakegh.Rule{Match: `^pr list --repo Tester/wakuwaku-site --head preview`, Out: "7\n"}, // PR はもうある
		fakegh.Rule{Match: `^run list --repo Tester/alpha --workflow`, Out: `[{"createdAt":"` + now + `","status":"in_progress","url":"x"}]`, Times: 1},
	)
	if _, err := capture(t, func() error { return cmdDeploy(nil) }); err != nil {
		t.Fatal(err)
	}
	if p := d.portfolio().Active(); p.Preview != "https://preview-wakuwaku-site.tester.workers.dev/alpha/" {
		t.Fatal(p.Preview)
	}
	if d.gh.Called("pr create") {
		t.Fatal("PR を二重に作った")
	}
}

// ---- ship (Web) の失敗 ----

func TestShipWebErrors(t *testing.T) {
	ok := []fakegh.Rule{
		{Match: `^api repos/Tester/(alpha|other)/readme`, Out: "https://wakuwaku-site.tester.workers.dev/alpha/ https://wakuwaku-site.tester.workers.dev/other/"},
		{Match: `^api repos/Tester/(alpha|other)/contents/`, Out: "x"},
		{Match: `^repo view Tester/(alpha|other) --json description`, Out: "3 2"},
		{Match: `^run list --repo Tester/(alpha|other) --branch main`, Out: "success"},
	}
	cases := map[string]struct {
		rules   []fakegh.Rule
		preview bool
		setup   func(d *delivery)
		want    string
	}{
		"preview ブランチがない": {want: "preview ブランチがない"},
		"preview にアプリがない": {preview: true, setup: func(d *delivery) { d.projects(shipping("Tester/other")) }, want: "preview に other がない"},
		"site が開けない":      {preview: true, setup: func(d *delivery) { os.RemoveAll(d.siteGit) }, want: "fetch"},
		"前の版を消せない":        {preview: true, setup: func(d *delivery) { failGit(d.t, "^rm -r -q --ignore-unmatch") }, want: "rm"},
		"preview から写せない":  {preview: true, setup: func(d *delivery) { failGit(d.t, "^checkout origin/preview") }, want: "checkout"},
		"一覧を書けない": {preview: true, setup: func(d *delivery) {
			if os.Geteuid() == 0 {
				d.t.Skip("root は権限で止まらない")
			}
			afterGit(d.t, "^checkout origin/preview", "chmod 444 "+filepath.Join(d.runtime, "site", "public", "index.html"))
		}, want: "index.html"},
		"push できない": {preview: true, setup: func(d *delivery) { failGit(d.t, "^push") }, want: "push"},
		"本番のビルドが失敗": {preview: true, rules: []fakegh.Rule{{Match: `check-runs`, Out: "W\tcompleted\tsuccess\n", Times: 1}, {Match: `check-runs`, Out: "W\tcompleted\tfailure\n"}}, want: "ビルドが失敗"},
		"本番の中身が違う":  {preview: true, setup: func(d *delivery) { cloudflare(d.t, d.siteGit, false) }, want: "検査に通らない"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d := newDelivery(t, append(c.rules, ok...)...)
			if c.preview {
				_, err := capture(t, func() error { return cmdDeploy(nil) })
				must(t, err)
			}
			d.projects(shipping("Tester/alpha"))
			if c.setup != nil {
				c.setup(d)
			}
			errHas(t, cmdShip(nil), c.want)
		})
	}
}

func shipping(repo string) *state.Project {
	return active(func(p *state.Project) { p.Repo, p.Status, p.Verdict = repo, state.Evaluating, "ship" })
}

func TestUnpublishErrors(t *testing.T) {
	cases := map[string]func(d *delivery){
		"site が開けない": func(d *delivery) { os.RemoveAll(d.siteGit) },
		"消せない":       func(d *delivery) { failGit(d.t, "^rm -r -q public/alpha") },
		"一覧を書けない": func(d *delivery) {
			if os.Geteuid() == 0 {
				d.t.Skip("root は権限で止まらない")
			}
			// 消したあとで一覧を書けなくする (git rm のすぐあと)
			afterGit(d.t, "^rm -r -q public/alpha", "chmod 444 "+filepath.Join(d.runtime, "site", "public", "index.html"))
		},
		"push できない": func(d *delivery) { failGit(d.t, "^push") },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			d := newDelivery(t)
			// 本番に alpha が置いてある状態を作る
			e, err := load()
			must(t, err)
			s, err := openSite(e)
			must(t, err)
			must(t, os.MkdirAll(filepath.Join(s.dir, "public", "alpha"), 0o755))
			must(t, os.WriteFile(filepath.Join(s.dir, "public", "alpha", "index.html"), []byte("x"), 0o644))
			_, err = s.commitPush("main", "ship: alpha", false)
			must(t, err)
			breakIt(d)
			if err := unpublishWeb(e, d.portfolio().Active()); err == nil {
				t.Fatal("失敗するはず")
			}
		})
	}
}

// ---- deploy (CLI): タグを打つ ----

func TestReleaseGo(t *testing.T) {
	d := newDelivery(t)
	d.projects(active(func(p *state.Project) { p.Stack = "go-cli" }))
	out, err := capture(t, func() error { return cmdDeploy(nil) })
	if err != nil || !strings.Contains(out, "releases/tag/v0.1.0") {
		t.Fatal(out, err)
	}
	_, err = capture(t, func() error { return cmdDeploy(nil) })
	must(t, err)
	if tags, _ := runGit(filepath.Join(d.remote, "Tester", "alpha.git"), "tag", "--list"); !strings.Contains(tags, "v0.2.0") {
		t.Fatal("2 回目は v0.2.0 のはず:", tags)
	}
	if d.portfolio().Active().URL != "https://github.com/Tester/alpha/releases/tag/v0.2.0" {
		t.Fatal(d.portfolio().Active().URL)
	}
}

func TestReleaseGoErrors(t *testing.T) {
	cases := map[string]struct {
		setup func(d *delivery)
		want  string
	}{
		"タグが読めない形":        {func(d *delivery) { _, _ = runGit(d.app, "tag", "vX") }, "読めない"},
		"タグを打てない":         {func(d *delivery) { failGit(d.t, "^tag -a") }, "tag"},
		"push できない":       {func(d *delivery) { failGit(d.t, "^push origin v") }, "push"},
		"portfolio が書けない": {func(d *delivery) { d.block("state/portfolio.json.tmp") }, "portfolio.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d := newDelivery(t)
			d.projects(active(func(p *state.Project) { p.Stack = "go-cli" }))
			c.setup(d)
			_, err := capture(t, func() error { return cmdDeploy(nil) })
			errHas(t, err, c.want)
		})
	}
}

// ---- dist の検査の残りの道 ----

func TestCopyDistMoreErrors(t *testing.T) {
	mkdist := func() string {
		d := t.TempDir()
		must(t, os.WriteFile(filepath.Join(d, "index.html"), []byte("x"), 0o644))
		return d
	}
	// 普通のファイルでない (名前つきパイプ)
	src := mkdist()
	must(t, syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644))
	errHas(t, copyDist(src, filepath.Join(t.TempDir(), "o")), "普通のファイルでない")
	// 大きすぎる (中身は書かずに大きさだけ)
	src = mkdist()
	big, err := os.Create(filepath.Join(src, "big.bin"))
	must(t, err)
	must(t, big.Truncate(maxFileBytes+1))
	big.Close()
	errHas(t, copyDist(src, filepath.Join(t.TempDir(), "o")), "大きすぎる")
	// 写し先にぶつかるものがある: ディレクトリの場所にファイル・ファイルの場所にディレクトリ
	src = mkdist()
	must(t, os.MkdirAll(filepath.Join(src, "assets"), 0o755))
	dst := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dst, "assets"), nil, 0o644))
	if copyDist(src, dst) == nil {
		t.Fatal("ディレクトリを作れないのに成功")
	}
	dst = t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dst, "index.html"), 0o755))
	if copyDist(mkdist(), dst) == nil {
		t.Fatal("書けないのに成功")
	}
	if os.Geteuid() == 0 {
		t.Skip("root は権限で止まらない")
	}
	// 読めないファイル・読めないディレクトリ
	src = mkdist()
	must(t, os.WriteFile(filepath.Join(src, "secret"), []byte("x"), 0))
	if copyDist(src, filepath.Join(t.TempDir(), "o")) == nil {
		t.Fatal("読めないファイルがあるのに成功")
	}
	src = mkdist()
	must(t, os.MkdirAll(filepath.Join(src, "locked", "x"), 0o755))
	must(t, os.Chmod(filepath.Join(src, "locked"), 0))
	t.Cleanup(func() { os.Chmod(filepath.Join(src, "locked"), 0o755) })
	if copyDist(src, filepath.Join(t.TempDir(), "o")) == nil {
		t.Fatal("読めないディレクトリがあるのに成功")
	}
}

func TestSmokeMoreProblems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "script-src 'unsafe-inline'")
		w.WriteHeader(500)
	}))
	defer srv.Close()
	got := strings.Join(smoke(srv.URL, nil), "\n")
	if !strings.Contains(got, "ステータス 500") || !strings.Contains(got, "unsafe-inline") {
		t.Fatal(got)
	}
	if p := smoke("http://127.0.0.1:1/", nil); len(p) != 1 {
		t.Fatal("つながらないのを見逃した", p)
	}
	if httpStatus("http://127.0.0.1:1/") != 0 {
		t.Fatal("つながらないのに 0 でない")
	}
	if tail("a\nb\nc\n", 2) != "b\nc" || tail("a", 5) != "a" {
		t.Fatal("tail")
	}
}

// ---- site init ----

func TestSiteInit(t *testing.T) {
	f := newFactory(t, fakegh.Rule{Match: `^repo view Tester/wakuwaku-site `, Code: 1})
	f.write("templates/site/README.md", "# site\n")
	f.write("templates/site/public/site.css", "body{}\n")
	must(t, os.MkdirAll(filepath.Join(f.remote, "Tester"), 0o755))
	_, err := runGit(filepath.Join(f.remote, "Tester"), "init", "-q", "--bare", "wakuwaku-site.git")
	must(t, err)
	out, err := capture(t, func() error { return cmdSite([]string{"init"}) })
	if err != nil || !strings.Contains(out, "作った") {
		t.Fatal(out, err)
	}
	if got, _ := runGit(filepath.Join(f.remote, "Tester", "wakuwaku-site.git"), "show", "main:public/index.html"); !strings.Contains(got, "まだありません") {
		t.Fatal(got)
	}
}

func TestSiteInitErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		rules []fakegh.Rule
		setup func(f *factory)
		want  string
	}{
		"使い方":         {args: []string{"x"}, want: "使い方"},
		"設定が壊れている":    {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
		"repo の名前がない": {setup: func(f *factory) { f.writeConfig(map[string]any{"site": map[string]string{"repo": "", "url": ""}}) }, want: "site.repo が空"},
		"一時ディレクトリがない": {setup: func(f *factory) { f.t.Setenv("TMPDIR", "/nonexistent-dir") }, want: "nonexistent"},
		"雛形がない":       {setup: func(f *factory) { os.RemoveAll(filepath.Join(f.root, "templates")) }, want: "templates"},
		"一覧を書けない":     {setup: func(f *factory) { f.write("templates/site/public/index.html/x", "") }, want: "index.html"},
		"git が使えない":   {setup: func(f *factory) { failGit(f.t, "^init") }, want: "init"},
		"すでに中身がある":    {rules: []fakegh.Rule{{Match: `^repo view Tester/wakuwaku-site `}, {Match: `^api repos/Tester/wakuwaku-site/commits`, Out: "abc"}}, want: "中身もある"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t, append(c.rules, fakegh.Rule{Match: `^repo view Tester/wakuwaku-site `, Code: 1})...)
			f.write("templates/site/README.md", "# site\n")
			f.write("templates/site/public/site.css", "body{}\n")
			f.write("templates/site/public/site.css", "body{}\n")
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"init"}
			}
			_, err := capture(t, func() error { return cmdSite(args) })
			errHas(t, err, c.want)
		})
	}
}

func TestPreviewCandidatesBadURL(t *testing.T) {
	if previewCandidates("://bad", "") != nil {
		t.Fatal("壊れた URL から候補を作った")
	}
	if !strings.Contains(string(renderIndex([]string{"ok-app", "Bad Name"})), "ok-app") {
		t.Fatal("一覧")
	}
}

func TestSmokeDoesNotFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example/", http.StatusFound)
	}))
	defer srv.Close()
	if got := strings.Join(smoke(srv.URL, nil), "\n"); !strings.Contains(got, "ステータス 302") {
		t.Fatal("リダイレクトを追いかけた", got)
	}
}

func TestPutAppCannotRemoveOldVersion(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root は権限で止まらない")
	}
	s := &site{dir: t.TempDir()}
	sub := filepath.Join(s.dir, "public", "alpha", "sub")
	must(t, os.MkdirAll(sub, 0o755))
	must(t, os.WriteFile(filepath.Join(sub, "f"), nil, 0o644))
	must(t, os.Chmod(sub, 0o555))
	t.Cleanup(func() { os.Chmod(sub, 0o755) })
	dist := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dist, "index.html"), []byte("x"), 0o644))
	if s.putApp("alpha", dist) == nil {
		t.Fatal("古い版を消せないのに成功")
	}
}

func TestIndexKeepsOtherShippedApps(t *testing.T) {
	newFactory(t)
	e, err := load()
	must(t, err)
	e.pf.Projects = []*state.Project{
		{Repo: "Tester/beta", Status: state.Shipped, Stack: "web"},
		{Repo: "Tester/cli", Status: state.Shipped, Stack: "go-cli"},
		{Repo: "Tester/alpha", Status: state.Shipped, Stack: "web"},
	}
	s := &site{dir: t.TempDir()}
	must(t, os.MkdirAll(filepath.Join(s.dir, "public"), 0o755))
	must(t, s.writeIndex(e, "alpha", false)) // alpha を捨てた
	b, _ := os.ReadFile(filepath.Join(s.dir, "public", "index.html"))
	if !strings.Contains(string(b), "/beta/") || strings.Contains(string(b), "/alpha/") || strings.Contains(string(b), "/cli/") {
		t.Fatal(string(b))
	}
}

// afterGit は、引数が re に合う git の呼び出しのあとで shell を動かす (git 自体は本物で、成功もそのまま返す)。
func afterGit(t *testing.T, re, shell string) {
	t.Helper()
	real, err := exec.LookPath("git")
	must(t, err)
	d := t.TempDir()
	sh := "#!/bin/sh\n" + real + " \"$@\"\nrc=$?\nif printf '%s' \"$*\" | grep -Eq '" + re + "'; then " + shell + "; fi\nexit $rc\n"
	must(t, os.WriteFile(filepath.Join(d, "git"), []byte(sh), 0o755))
	t.Setenv("PATH", d+string(os.PathListSeparator)+os.Getenv("PATH"))
}
