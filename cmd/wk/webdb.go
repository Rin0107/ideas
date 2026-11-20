package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/state"
)

// web-db (Cloudflare Worker + D1、主力のアプリ) の配信 (docs/DESIGN-v3.md §10・docs/SETUP.md の web-db):
//   - アプリのビルド (npm ci・verify・build・bundle) は手元で、鍵らしい環境変数を消して動かす (web と同じ)
//   - できたもの (dist・bundle/worker.js・migrations) と、wk が作る wrangler.jsonc・package.json (版固定の wrangler だけ) を、
//     アプリの repo の deploy ブランチに 1 コミットで force push する。エージェントの書いた package.json・wrangler の設定は入れない
//   - Cloudflare の Workers Builds が deploy ブランチを本番に出す (D1 の migration を当ててから wrangler deploy)。
//     鍵のある Cloudflare のビルドで動くのは版固定の wrangler だけ。worker.js は Worker の中でだけ動く
//   - web-db は deploy が本番 (プレビューはない)。主力のアプリは ship しない

// wranglerVersion は deploy ブランチで Cloudflare が動かす wrangler の版 (雛形と同じ)。
const wranglerVersion = "4.158.0"

// webDBBuild は web-db のアプリのビルド。dist/ (画面) と bundle/worker.js (Worker) を作る。
var webDBBuild = [][]string{
	{"npm", "ci", "--no-audit", "--no-fund", "--ignore-scripts"},
	{"npm", "run", "verify"},
	{"npm", "run", "build"},
	{"npm", "run", "bundle"},
}

// stageWrite は deploy ブランチの中身を書く (テストで失敗させる)。
var stageWrite = os.WriteFile

// deployWebDB はアプリをビルドして deploy ブランチに置き、Cloudflare のビルドを待って、本番の URL を確かめる。
func deployWebDB(e *env, p *state.Project, dir string) error {
	url, id := e.cfg.WebDB.URL(p.Repo), e.cfg.WebDB.D1[p.Repo]
	if url == "" || id == "" {
		return errors.New(p.Repo + " の Cloudflare の準備 (D1 と Workers Builds の連携) がまだ。人の作業: docs/SETUP.md の web-db の節を " +
			"wk human --title で頼み、factory.json の web_db (subdomain と d1 の database_id) を書いてもらう")
	}
	if err := runBuild(dir, webDBBuild); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "wk-deploy-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	want, err := stageWebDB(dir, stage, filepath.Base(p.Repo), id, url)
	if err != nil {
		return err
	}
	sha, err := pushDeploy(dir, stage, e.cfg.Owner)
	if err != nil {
		return err
	}
	if err := (&site{repo: p.Repo}).waitChecks(sha); err != nil {
		return err
	}
	if probs := smoke(url, want); len(probs) > 0 {
		return fmt.Errorf("%s の検査に通らない:\n  %s", url, strings.Join(probs, "\n  "))
	}
	if p.URL != url || p.Preview != url {
		// 関数を渡す形の後ろの err != nil はミューテーションがカバレッジの外と数えるので、関数を先に作る
		set := func(q *state.Project) { q.URL, q.Preview = url, url; e.event(q, "deployed", url) }
		if err := e.update(p, set); err != nil {
			return err
		}
	}
	fmt.Println("本番に出した:", url)
	return nil
}

// stageWebDB は deploy ブランチの中身を stage に作り、置いた index.html を返す。
func stageWebDB(dir, stage, name, id, url string) ([]byte, error) {
	if err := copyDist(filepath.Join(dir, "dist"), filepath.Join(stage, "public")); err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	worker, err := regularFile(filepath.Join(dir, "bundle", "worker.js"))
	if err != nil {
		return nil, fmt.Errorf("bundle/worker.js (npm run bundle が作る): %w", err)
	}
	files["worker.js"] = worker
	sqls, _ := filepath.Glob(filepath.Join(dir, "migrations", "*.sql"))
	if len(sqls) == 0 {
		return nil, errors.New("migrations/*.sql がない (D1 の表は migration で作る)")
	}
	for _, f := range sqls {
		b, err := regularFile(f)
		if err != nil {
			return nil, fmt.Errorf("migrations/%s: %w", filepath.Base(f), err)
		}
		files[filepath.Join("migrations", filepath.Base(f))] = b
	}
	files["package.json"], _ = json.MarshalIndent(map[string]any{
		"name": name + "-deploy", "private": true, "devDependencies": map[string]string{"wrangler": wranglerVersion},
	}, "", "  ")
	files["wrangler.jsonc"], _ = json.MarshalIndent(webDBWrangler(name, id, url), "", "  ")
	files["README.md"] = []byte("# " + name + " (配信用)\n\nwk deploy が作るブランチ。手で変えない (docs/DESIGN-v3.md §10)。\n")
	for path, b := range files {
		full := filepath.Join(stage, path)
		err := os.MkdirAll(filepath.Dir(full), 0o755)
		if err == nil {
			err = stageWrite(full, b, 0o644)
		}
		if err != nil {
			return nil, err
		}
	}
	return os.ReadFile(filepath.Join(stage, "public", "index.html"))
}

// webDBWrangler は deploy ブランチの wrangler の設定。wk が決める (アプリの側の設定は使わない)。
func webDBWrangler(name, id, url string) map[string]any {
	return map[string]any{
		"name": name, "main": "worker.js", "no_bundle": true,
		"compatibility_date": "2026-10-01", "compatibility_flags": []string{"nodejs_compat"},
		"assets": map[string]any{"directory": "./public", "binding": "ASSETS", "run_worker_first": true,
			"not_found_handling": "single-page-application"},
		"d1_databases":  []map[string]string{{"binding": "DB", "database_name": name, "database_id": id, "migrations_dir": "migrations"}},
		"vars":          map[string]string{"ORIGIN": strings.TrimRight(url, "/")},
		"observability": map[string]bool{"enabled": false},
		"workers_dev":   true, "preview_urls": false,
	}
}

// regularFile は普通のファイル (リンクでない、10MB まで) の中身を返す。
func regularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil, errors.New("普通のファイルでない・大きすぎる")
	}
	return os.ReadFile(path)
}

// pushDeploy は stage を 1 コミットにして、アプリの repo の deploy ブランチに force push し、コミットの SHA を返す。
func pushDeploy(dir, stage, owner string) (string, error) {
	remote, err := gh.Run(dir, "git", "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	for _, c := range [][]string{
		{"init", "-q", "-b", "deploy"},
		{"add", "-A"},
		{"-c", "user.name=" + owner, "-c", "user.email=" + owner + "@users.noreply.github.com", "commit", "-q", "-m", "deploy (wk deploy が作る)"},
		{"push", "-q", "--force", strings.TrimSpace(remote), "HEAD:refs/heads/deploy"},
	} {
		if _, err := gh.Run(stage, "git", c...); err != nil {
			return "", err
		}
	}
	sha, err := gh.Run(stage, "git", "rev-parse", "HEAD")
	return strings.TrimSpace(sha), err
}
