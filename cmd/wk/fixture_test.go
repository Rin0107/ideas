package main

// cmd/wk のテストの土台: 一時ディレクトリに工場を作り、偽物の gh (internal/fakegh) を置く。
// git は本物を使うが、https://github.com/ は手元の bare repo に向ける (ネットワークに出ない)。

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rin0107/ideas/internal/fakegh"
	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/next"
	"github.com/Rin0107/ideas/internal/state"
)

type factory struct {
	t                                 *testing.T
	root, runtime, apps, home, remote string
	gh                                *fakegh.Fake
}

// newFactory は工場を作り、そこへ移動する。rules は偽物の gh の決まり (最後に「何にでも [] を返す」が付く)。
func newFactory(t *testing.T, rules ...fakegh.Rule) *factory {
	t.Helper()
	base := t.TempDir()
	f := &factory{t: t, root: filepath.Join(base, "factory"), runtime: filepath.Join(base, "runtime"),
		apps: filepath.Join(base, "apps"), home: filepath.Join(base, "home"), remote: filepath.Join(base, "remote")}
	for _, d := range []string{f.root, f.runtime, f.apps, f.home, f.remote} {
		must(t, os.MkdirAll(d, 0o755))
	}
	t.Setenv("HOME", f.home)
	t.Setenv("WK_FACTORY", "")
	cfg := gitconfig(t, f.remote)
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	f.writeConfig(nil)
	f.write("state/meta.json", `{"last_intake":"2999-01-01","last_retro":"2999-01-01"}`)
	f.write("state/portfolio.json", `{"projects":[]}`)
	var index strings.Builder
	index.WriteString("# 索引\n\n")
	for i, name := range []string{"00001-alpha", "00002-beta", "00003-gamma"} {
		f.write("ideas_wakuwaku/"+name+"/idea.md", fmt.Sprintf(idea, "アイディア"+fmt.Sprint(i)))
		fmt.Fprintf(&index, "- %s %s — 一言\n", name[:5], name[6:])
	}
	f.write("ideas_wakuwaku/INDEX.md", index.String())
	f.gh = fakegh.Install(t, append(rules, fakegh.Rule{Match: `.`, Out: "[]"})...)
	// 個人情報の検査は本物の repo を clone するので、ふだんは通す (TestPrivacyScan が本物を見る)
	privacyScan = func(*env, string) []string { return nil }
	t.Cleanup(func() { privacyScan = realPrivacyScan })
	fakeMachine(t, roomyMem, idleLoad)
	t.Chdir(f.root)
	return f
}

const (
	roomyMem = "MemAvailable: 8388608 kB\n" // 8GB 空いている
	idleLoad = "0.00 0.00 0.00 1/1 1\n"
)

// fakeMachine は wk due が読む /proc を偽物にする (本物の機械の混み具合でテストが変わらないように)。
func fakeMachine(t *testing.T, meminfo, loadavg string) {
	r := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(r, "proc"), 0o755))
	must(t, os.WriteFile(filepath.Join(r, "proc/meminfo"), []byte(meminfo), 0o644))
	must(t, os.WriteFile(filepath.Join(r, "proc/loadavg"), []byte(loadavg), 0o644))
	old := sysRoot
	sysRoot = r
	t.Cleanup(func() { sysRoot = old })
}

// gitconfig は https://github.com/<x> を <remote>/<x> に向ける git の設定を作る。
func gitconfig(t *testing.T, remote string) string {
	p := filepath.Join(t.TempDir(), "gitconfig")
	must(t, os.WriteFile(p, []byte("[user]\n\tname = t\n\temail = t@example.com\n[init]\n\tdefaultBranch = main\n"+
		"[url \""+remote+"/\"]\n\tinsteadOf = https://github.com/\n"), 0o644))
	return p
}

// writeConfig は factory.json を書く。over で値を上書きする。
func (f *factory) writeConfig(over map[string]any) {
	cfg := map[string]any{
		"owner": "Tester", "main_checkout": f.root, "apps_dir": f.apps, "runtime_dir": f.runtime,
		"ideas_dir": "ideas_wakuwaku", "ticks_per_day": 6, "max_ticks_mvp": 30, "max_ticks_iteration": 15,
		"max_iterations": 2, "max_attempts_per_issue": 3, "max_open_issues": 8, "lock_ttl_minutes": 150,
		"max_consecutive_failures": 3, "intake_every_days": 7, "retro_every_days": 7, "min_untried_ideas": 1,
		"select":          map[string]int{"candidates": 15, "min_free": 3, "min_light": 3},
		"evaluate":        map[string]int{"ship_min_wakuwaku": 4, "ship_min_quality": 3},
		"protected_paths": []string{"factory.json"},
		"site":            map[string]string{"repo": "Tester/wakuwaku-site", "url": "https://wakuwaku-site.tester.workers.dev"},
	}
	for k, v := range over {
		cfg[k] = v
	}
	b, err := json.Marshal(cfg)
	must(f.t, err)
	f.write("factory.json", string(b))
}

// write は工場の中にファイルを書く (rel は工場からの相対パス、または絶対パス)。
func (f *factory) write(rel, body string) {
	f.t.Helper()
	p := rel
	if !filepath.IsAbs(p) {
		p = filepath.Join(f.root, rel)
	}
	must(f.t, os.MkdirAll(filepath.Dir(p), 0o755))
	must(f.t, os.WriteFile(p, []byte(body), 0o644))
}

// block は p をディレクトリにして、ファイルとして読み書きできなくする (失敗の道を通すため)。
func (f *factory) block(p string) {
	f.t.Helper()
	if !filepath.IsAbs(p) {
		p = filepath.Join(f.root, p)
	}
	_ = os.Remove(p)
	must(f.t, os.MkdirAll(filepath.Join(p, "x"), 0o755))
}

// projects は portfolio を書く。
func (f *factory) projects(ps ...*state.Project) {
	f.t.Helper()
	must(f.t, state.SavePortfolio(f.root, &state.Portfolio{Projects: ps}))
}

func (f *factory) portfolio() *state.Portfolio {
	f.t.Helper()
	pf, err := state.LoadPortfolio(f.root)
	must(f.t, err)
	return pf
}

func (f *factory) read(rel string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.root, rel))
	must(f.t, err)
	return string(b)
}

// capture は fn の標準出力を返す。
func capture(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	must(t, err)
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	ferr := fn()
	os.Stdout = old
	w.Close()
	return <-done, ferr
}

// active は作っている途中のアプリ (既定: web・building・MVP)。
func active(mut ...func(*state.Project)) *state.Project {
	p := &state.Project{Idea: "00001-alpha", Repo: "Tester/alpha", Status: state.Building, Stack: "web",
		Started: "2026-11-10", Milestone: "MVP"}
	for _, m := range mut {
		m(p)
	}
	return p
}

func nextAction(phase string) next.Action { return next.Action{Phase: phase, Reason: "テスト"} }

func runGit(dir string, args ...string) (string, error) { return gh.Run(dir, "git", args...) }
