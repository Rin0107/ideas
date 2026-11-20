package main

// web-db (Worker + D1、主力のアプリ) の配信のテスト。アプリの repo の deploy ブランチに、ビルドしたものと
// wk が作る wrangler の設定を置き、Cloudflare (手元の TLS サーバー) が deploy ブランチを配る。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/state"
)

const workerShell = "mkdir -p bundle && echo 'export default {}' > bundle/worker.js"

// newWebDB は web-db のアプリ alpha (D1 の準備済み) をそろえる。rules は既定 (Cloudflare のビルドは成功) より先に効く。
func newWebDB(t *testing.T, rules ...fakegh.Rule) *delivery {
	t.Helper()
	d := newDelivery(t, append(rules,
		fakegh.Rule{Match: `^api repos/Tester/alpha/commits/[0-9a-f]+/check-runs`, Out: "Workers Builds: alpha\tcompleted\tsuccess\n"})...)
	d.writeConfig(map[string]any{"web_db": map[string]any{"subdomain": "tester", "d1": map[string]string{"Tester/alpha": "db-1"}}})
	d.projects(active(func(p *state.Project) { p.Stack = "web-db" }))
	fakeNpm(t, "<p>closet v1</p>", "", workerShell)
	d.write(filepath.Join(d.app, "migrations", "0001_init.sql"), "CREATE TABLE clothes (id TEXT);\n")
	return d
}

func (d *delivery) deployFile(path string) string {
	out, _ := runGit(filepath.Join(filepath.Dir(d.siteGit), "alpha.git"), "show", "deploy:"+path)
	return out
}

func TestDeployWebDB(t *testing.T) {
	d := newWebDB(t)
	out, err := capture(t, func() error { return cmdDeploy(nil) })
	if err != nil || !strings.Contains(out, "本番に出した: https://alpha.tester.workers.dev/") {
		t.Fatal(out, err)
	}
	for path, want := range map[string][]string{
		"wrangler.jsonc": {`"name": "alpha"`, `"main": "worker.js"`, `"no_bundle": true`, `"database_id": "db-1"`,
			`"database_name": "alpha"`, `"migrations_dir": "migrations"`, `"ORIGIN": "https://alpha.tester.workers.dev"`,
			`"directory": "./public"`, `"run_worker_first": true`, `"preview_urls": false`, `"enabled": false`},
		"package.json":             {`"wrangler": "` + wranglerVersion + `"`},
		"worker.js":                {"export default {}"},
		"migrations/0001_init.sql": {"CREATE TABLE clothes"},
		"public/index.html":        {"<p>closet v1</p>"},
		"README.md":                {"手で変えない"},
	} {
		got := d.deployFile(path)
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Fatalf("deploy:%s に %q がない:\n%s", path, w, got)
			}
		}
	}
	if strings.Contains(d.deployFile("package.json"), "scripts") {
		t.Fatal("deploy ブランチの package.json に scripts がある")
	}
	p := d.portfolio().Active()
	if p.URL != "https://alpha.tester.workers.dev/" || p.Preview != p.URL {
		t.Fatalf("%+v", p)
	}
	// もう一度出しても、記録は増えず、deploy ブランチは 1 コミットのまま
	n := len(p.History)
	_, err = capture(t, func() error { return cmdDeploy(nil) })
	must(t, err)
	if len(d.portfolio().Active().History) != n {
		t.Fatal("同じ URL を二重に記録した")
	}
	if c, _ := runGit(filepath.Join(filepath.Dir(d.siteGit), "alpha.git"), "rev-list", "--count", "deploy"); strings.TrimSpace(c) != "1" {
		t.Fatal("deploy ブランチのコミット:", c)
	}
	// コミットの名前とメールは owner の GitHub のユーザー名と noreply (本名やほかの名前を載せない)
	if who, _ := runGit(filepath.Join(filepath.Dir(d.siteGit), "alpha.git"), "log", "-1", "--format=%an <%ae> %cn <%ce>", "deploy"); strings.TrimSpace(who) !=
		"Tester <Tester@users.noreply.github.com> Tester <Tester@users.noreply.github.com>" {
		t.Fatal("deploy のコミットの名前:", who)
	}
	// URL だけ違う記録 (preview は同じ) も直す
	d.projects(active(func(q *state.Project) { q.Stack, q.Preview = "web-db", "https://alpha.tester.workers.dev/" }))
	_, err = capture(t, func() error { return cmdDeploy(nil) })
	must(t, err)
	if d.portfolio().Active().URL != "https://alpha.tester.workers.dev/" {
		t.Fatal("URL を記録していない")
	}
	// web-db は ship しない (deploy が本番)
	errHas(t, cmdShip(nil), "web-db は wk deploy が本番に出す")
}

func TestDeployWebDBErrors(t *testing.T) {
	cases := map[string]struct {
		rules []fakegh.Rule
		setup func(d *delivery)
		want  string
	}{
		"D1 の準備がまだ": {setup: func(d *delivery) {
			d.writeConfig(map[string]any{"web_db": map[string]any{"subdomain": "tester"}})
		}, want: "Cloudflare の準備 (D1 と Workers Builds の連携) がまだ"},
		"サブドメインがまだ": {setup: func(d *delivery) {
			d.writeConfig(map[string]any{"web_db": map[string]any{"d1": map[string]string{"Tester/alpha": "db-1"}}})
		}, want: "Cloudflare の準備"},
		"ビルドが失敗":        {setup: func(d *delivery) { fakeNpm(d.t, "x", "bundle", workerShell) }, want: "npm run bundle が失敗"},
		"置き場所を作れない":     {setup: func(d *delivery) { d.t.Setenv("TMPDIR", filepath.Join(d.root, "none", "tmp")) }, want: "none/tmp"},
		"dist を置けない":    {setup: func(d *delivery) { fakeNpm(d.t, "x", "", workerShell+" && touch dist/_headers") }, want: "_headers"},
		"worker.js がない": {setup: func(d *delivery) { fakeNpm(d.t, "x", "", "") }, want: "bundle/worker.js"},
		"migration がない": {setup: func(d *delivery) { os.RemoveAll(filepath.Join(d.app, "migrations")) }, want: "migrations/*.sql がない"},
		"migration がリンク": {setup: func(d *delivery) {
			must(d.t, os.Symlink("/etc/hostname", filepath.Join(d.app, "migrations", "0002_x.sql")))
		}, want: "migrations/0002_x.sql: 普通のファイルでない"},
		"書けない": {setup: func(d *delivery) {
			old := stageWrite
			stageWrite = func(string, []byte, os.FileMode) error { return errors.New("書けない") }
			d.t.Cleanup(func() { stageWrite = old })
		}, want: "書けない"},
		"remote が読めない":       {setup: func(d *delivery) { failGit(d.t, "^remote get-url") }, want: "remote get-url"},
		"push できない":          {setup: func(d *delivery) { failGit(d.t, "^push -q --force") }, want: "push"},
		"SHA が読めない":          {setup: func(d *delivery) { failGit(d.t, "^rev-parse HEAD") }, want: "rev-parse"},
		"Cloudflare のビルドが失敗": {rules: []fakegh.Rule{{Match: `check-runs`, Out: "Workers Builds\tcompleted\tfailure\n"}}, want: "Cloudflare のビルドが失敗"},
		"本番に守りのヘッダーがない":      {setup: func(d *delivery) { cloudflare(d.t, d.siteGit, false) }, want: "https://alpha.tester.workers.dev/ の検査に通らない"},
		"portfolio が書けない":    {setup: func(d *delivery) { d.block("state/portfolio.json.tmp") }, want: "portfolio.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d := newWebDB(t, c.rules...)
			if c.setup != nil {
				c.setup(d)
			}
			_, err := capture(t, func() error { return cmdDeploy(nil) })
			errHas(t, err, c.want)
		})
	}
}

func TestRegularFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	must(t, os.WriteFile(p, nil, 0o644))
	must(t, os.Truncate(p, maxFileBytes))
	if _, err := regularFile(p); err != nil {
		t.Fatal("ちょうど上限は通る", err)
	}
	must(t, os.Truncate(p, maxFileBytes+1))
	if _, err := regularFile(p); err == nil {
		t.Fatal("上限を超えても通った")
	}
	if _, err := regularFile(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Fatal("ないのに通った")
	}
}
