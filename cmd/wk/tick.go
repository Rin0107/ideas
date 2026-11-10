package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/guard"
	"github.com/Rin0107/ideas/internal/ideas"
	"github.com/Rin0107/ideas/internal/lock"
	"github.com/Rin0107/ideas/internal/next"
	"github.com/Rin0107/ideas/internal/room"
	"github.com/Rin0107/ideas/internal/state"
)

// env は 1 回のコマンドで使う設定と状態。
type env struct {
	cfg *config.Config
	pf  *state.Portfolio
	now time.Time
}

func load() (*env, error) {
	wd, _ := os.Getwd()
	cfg, err := config.Load(wd)
	if err != nil {
		// アプリ repo の中から呼ばれたとき (フックなど) は工場の main を見る
		root := os.Getenv("WK_FACTORY")
		if root == "" {
			root = config.Expand("~/src/okuos/repos/ideas")
		}
		if cfg, err = config.Load(root); err != nil {
			return nil, err
		}
	}
	pf, err := state.LoadPortfolio(cfg.Root)
	if err != nil {
		return nil, err
	}
	return &env{cfg: cfg, pf: pf, now: time.Now()}, nil
}

func (e *env) factoryRepo() string { return e.cfg.Owner + "/" + filepath.Base(e.cfg.MainCheckout) }
func (e *env) today() string       { return e.now.Format(time.DateOnly) }
func (e *env) ttl() time.Duration  { return time.Duration(e.cfg.LockTTLMinutes) * time.Minute }

func (e *env) stopFile() string {
	for _, p := range []string{filepath.Join(e.cfg.RuntimeDir, "STOP"), filepath.Join(e.cfg.MainCheckout, "STOP")} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (e *env) snapshot() (next.Snapshot, error) {
	meta, err := state.LoadMeta(e.cfg.Root)
	if err != nil {
		return next.Snapshot{}, err
	}
	all, err := ideas.Load(filepath.Join(e.cfg.Root, e.cfg.IdeasDir))
	if err != nil {
		return next.Snapshot{}, err
	}
	s := next.Snapshot{Now: e.now, Cfg: e.cfg, Meta: meta, Active: e.pf.Active(), Untried: ideas.Untried(all, e.pf.Tried())}
	if p := s.Active; p != nil && p.Repo != "" && (p.Status == state.Building || p.Status == state.Iterating) {
		if s.Issues, err = gh.OpenIssues(p.Repo, p.Milestone); err != nil {
			return next.Snapshot{}, err
		}
	}
	return s, nil
}

func cmdDue(args []string) error {
	e, err := load()
	if err != nil {
		return err
	}
	if p := e.stopFile(); p != "" {
		return exitError{1, "STOP がある (" + p + ")"}
	}
	// 機械に余裕がなければ始めない (2026-11-10 にメモリが尽きて PC ごと落ちた)
	if why := room.Problems(sysRoot, runtime.NumCPU(), room.Default()); len(why) > 0 {
		return exitError{1, "機械に余裕がない: " + strings.Join(why, " / ")}
	}
	rt, err := state.LoadRuntime(e.cfg.RuntimeDir, e.now)
	if err != nil {
		return err
	}
	if rt.TicksToday >= e.cfg.TicksPerDay {
		return exitError{1, fmt.Sprintf("今日の tick は上限 (%d/%d)", rt.TicksToday, e.cfg.TicksPerDay)}
	}
	if l, err := lock.Read(e.cfg.RuntimeDir); err != nil {
		return err
	} else if l != nil && !l.Stale(e.now, e.ttl()) {
		return exitError{1, fmt.Sprintf("ほかの tick が動いている (%s から %s)", l.Since.Format(time.TimeOnly), l.Phase)}
	}
	fmt.Printf("ok (今日 %d/%d tick)\n", rt.TicksToday, e.cfg.TicksPerDay)
	return nil
}

func cmdNext(args []string) error {
	e, err := load()
	if err != nil {
		return err
	}
	if p := e.stopFile(); p != "" {
		return errors.New("STOP がある (" + p + ")")
	}
	s, err := e.snapshot()
	if err != nil {
		return err
	}
	a := next.Decide(s)
	wd, _ := os.Getwd()
	stale, err := lock.Acquire(e.cfg.RuntimeDir, lock.Info{Since: e.now, Phase: a.Phase, Worktree: wd}, e.ttl())
	if err != nil {
		return err
	}
	rt, err := state.LoadRuntime(e.cfg.RuntimeDir, e.now)
	if err != nil {
		return err
	}
	if stale != nil {
		rt.ConsecutiveFailures++
		_ = state.AppendTick(e.cfg.Root, state.Tick{Start: stale.Since, End: e.now, Phase: stale.Phase, Result: "aborted",
			Note: "ロックが " + stale.Worktree + " に残っていた"})
	}
	if a.Phase == state.PhaseIdle {
		_ = lock.Release(e.cfg.RuntimeDir)
		return printJSON(a)
	}
	rt.TicksToday++
	if err := state.SaveRuntime(e.cfg.RuntimeDir, rt); err != nil {
		return err
	}
	if p := e.pf.Active(); p != nil && a.Repo == p.Repo {
		p.Ticks.Total++
		if a.Phase == state.PhaseBuild {
			p.Ticks.Phase++
		}
		if err := state.SavePortfolio(e.cfg.Root, e.pf); err != nil {
			return err
		}
	}
	if err := writeCurrent(e, a); err != nil {
		return err
	}
	return printJSON(a)
}

// 今の tick の段階を runtime_dir に残す (done が読む)
func writeCurrent(e *env, a next.Action) error {
	b, _ := json.Marshal(struct {
		next.Action
		Start time.Time `json:"start"`
	}{a, e.now})
	return os.WriteFile(filepath.Join(e.cfg.RuntimeDir, "current.json"), b, 0o644)
}

func readCurrent(e *env) (next.Action, time.Time, error) {
	var c struct {
		next.Action
		Start time.Time `json:"start"`
	}
	b, err := os.ReadFile(filepath.Join(e.cfg.RuntimeDir, "current.json"))
	if err != nil {
		return next.Action{}, time.Time{}, errors.New("今の tick がない (wk next を先に実行する)")
	}
	err = json.Unmarshal(b, &c)
	return c.Action, c.Start, err
}

func cmdDone(args []string) error {
	fs := flag.NewFlagSet("done", flag.ContinueOnError)
	result := fs.String("result", "", "ok / fail")
	note := fs.String("note", "", "1 行のメモ")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *result != "ok" && *result != "fail" {
		return errors.New("--result は ok か fail")
	}
	e, err := load()
	if err != nil {
		return err
	}
	a, start, err := readCurrent(e)
	if err != nil {
		return err
	}
	rt, err := state.LoadRuntime(e.cfg.RuntimeDir, e.now)
	if err != nil {
		return err
	}
	if *result == "ok" {
		rt.ConsecutiveFailures = 0
		if a.Phase == state.PhaseIntake || a.Phase == state.PhaseRetro {
			m, err := state.LoadMeta(e.cfg.Root)
			if err != nil {
				return err
			}
			if a.Phase == state.PhaseIntake {
				m.LastIntake = e.today()
			} else {
				m.LastRetro = e.today()
			}
			if err := state.SaveMeta(e.cfg.Root, m); err != nil {
				return err
			}
		}
	} else {
		rt.ConsecutiveFailures++
	}
	if err := state.AppendTick(e.cfg.Root, state.Tick{Start: start, End: e.now, Phase: a.Phase, Repo: a.Repo,
		Issue: a.Issue, Result: *result, Note: *note}); err != nil {
		return err
	}
	if err := state.SaveRuntime(e.cfg.RuntimeDir, rt); err != nil {
		return err
	}
	if rt.ConsecutiveFailures >= e.cfg.MaxConsecutiveFailures {
		if err := stopWithIncident(e, rt, a, *note); err != nil {
			fmt.Fprintln(os.Stderr, "incident の作成に失敗:", err)
		}
	}
	_ = os.Remove(filepath.Join(e.cfg.RuntimeDir, "current.json"))
	if err := lock.Release(e.cfg.RuntimeDir); err != nil {
		return err
	}
	fmt.Printf("%s %s (連続失敗 %d/%d)\n", a.Phase, *result, rt.ConsecutiveFailures, e.cfg.MaxConsecutiveFailures)
	return nil
}

// stopWithIncident は STOP を置いて止まり、工場 repo に incident を立てる。人が来るまで tick は動かない。
func stopWithIncident(e *env, rt *state.Runtime, a next.Action, note string) error {
	msg := fmt.Sprintf("%s: 連続 %d 回失敗したので止まった。最後: %s %s #%d %s\n", e.now.Format(time.DateTime),
		rt.ConsecutiveFailures, a.Phase, a.Repo, a.Issue, note)
	if err := os.WriteFile(filepath.Join(e.cfg.RuntimeDir, "STOP"), []byte(msg), 0o644); err != nil {
		return err
	}
	body := msg + "\n直したら `" + filepath.Join(e.cfg.RuntimeDir, "STOP") + "` を消すと再開する。\n記録: state/ticks.jsonl"
	_, err := gh.GH("issue", "create", "--repo", e.factoryRepo(), "--title", "wk:incident 連続失敗で停止 "+e.today(),
		"--label", "wk:incident", "--body", body)
	return err
}

func cmdSync(args []string) error {
	e, err := load()
	if err != nil {
		return err
	}
	wd := e.cfg.Root
	if out, _ := gh.Run(wd, "git", "status", "--porcelain"); strings.TrimSpace(out) != "" {
		return errors.New("未コミットの変更がある。先にコミットする:\n" + out)
	}
	br, err := gh.Run(wd, "git", "branch", "--show-current")
	if err != nil {
		return err
	}
	br = strings.TrimSpace(br)
	if br == "" || br == "main" {
		return errors.New("worktree の作業ブランチで実行する")
	}
	main := e.cfg.MainCheckout
	if out, _ := gh.Run(main, "git", "status", "--porcelain", "--untracked-files=no"); strings.TrimSpace(out) != "" {
		return errors.New("工場の main に未コミットの変更がある: " + main)
	}
	if _, err := gh.Run(main, "git", "pull", "--ff-only", "origin", "main"); err != nil {
		return err
	}
	if out, err := gh.Run(wd, "git", "diff", "--name-only", "origin/main...HEAD"); err != nil {
		return err
	} else if hit := guard.TouchesProtected(strings.Split(out, "\n"), e.cfg.ProtectedPaths); len(hit) > 0 {
		return fmt.Errorf("守りの部分が変わっているので main に入れない (ユーザーが見る): %s", strings.Join(hit, ", "))
	}
	if _, err := gh.Run(main, "git", "merge", "--no-edit", br); err != nil {
		_, _ = gh.Run(main, "git", "merge", "--abort")
		return fmt.Errorf("merge が衝突した (中止した): %w", err)
	}
	if _, err := gh.Run(main, "git", "push", "origin", "main"); err != nil {
		return err
	}
	fmt.Println("main に merge して push した:", br)
	return nil
}

func cmdStatus(args []string) error {
	short := len(args) > 0 && args[0] == "--short"
	e, err := load()
	if err != nil {
		return err
	}
	rt, _ := state.LoadRuntime(e.cfg.RuntimeDir, e.now)
	stop := e.stopFile()
	p := e.pf.Active()
	if short {
		switch {
		case stop != "":
			b, _ := os.ReadFile(stop)
			fmt.Printf("wakuwaku: 停止中 — %s", b)
		case p != nil:
			fmt.Printf("wakuwaku: %s を %s (%d tick)\n", p.Idea, p.Status, p.Ticks.Total)
		default:
			fmt.Println("wakuwaku: 作っているものはない")
		}
		return nil
	}
	fmt.Printf("工場: %s\n停止: %v\n今日の tick: %d/%d (連続失敗 %d)\n", e.cfg.Root, stop != "", rt.TicksToday, e.cfg.TicksPerDay, rt.ConsecutiveFailures)
	if p != nil {
		fmt.Printf("作っているもの: %s (%s) %s %s tick %d (この段階 %d)\n", p.Idea, p.Repo, p.Status, p.Milestone, p.Ticks.Total, p.Ticks.Phase)
		if p.Preview != "" {
			fmt.Println("プレビューの URL:", p.Preview)
		}
		if p.URL != "" {
			fmt.Println("URL:", p.URL)
		}
		if p.Stack == "web" && p.Repo != "" {
			if e.cfg.Site.URL == "" {
				fmt.Println("公開時の URL: まだ決まらない (factory.json の site.url が空。docs/SETUP.md)")
			} else {
				fmt.Printf("公開時の URL: %s/%s/\n", strings.TrimRight(e.cfg.Site.URL, "/"), filepath.Base(p.Repo))
			}
		}
	}
	if e.cfg.Site.URL == "" {
		fmt.Println("配信: 準備がまだ (Web アプリは出せない。docs/SETUP.md の 2)")
	}
	n := map[string]int{}
	for _, q := range e.pf.Projects {
		n[q.Status]++
	}
	fmt.Printf("これまで: shipped %d / dropped %d\n", n[state.Shipped], n[state.Dropped])
	if s, err := e.snapshot(); err == nil {
		a := next.Decide(s)
		fmt.Printf("次の段階: %s %s #%d — %s\n", a.Phase, a.Repo, a.Issue, a.Reason)
	} else {
		fmt.Println("次の段階: 判定できない:", err)
	}
	return nil
}

func cmdCheck(args []string) error {
	e, err := load()
	if err != nil {
		return err
	}
	var errs []string
	errs = append(errs, e.pf.Validate()...)
	all, err := ideas.Load(filepath.Join(e.cfg.Root, e.cfg.IdeasDir))
	if err != nil {
		return err
	}
	errs = append(errs, ideas.Problems(all)...)
	if b, err := os.ReadFile(filepath.Join(e.cfg.Root, e.cfg.IdeasDir, "INDEX.md")); err != nil {
		errs = append(errs, "INDEX.md を読めない: "+err.Error())
	} else {
		errs = append(errs, ideas.IndexProblems(string(b), all)...)
	}
	known := map[string]bool{}
	for _, i := range all {
		known[i.Dir] = true
	}
	for _, p := range e.pf.Projects {
		if !known[p.Idea] {
			errs = append(errs, "portfolio の "+p.Idea+" が "+e.cfg.IdeasDir+" にない")
		}
	}
	if _, err := state.ReadTicks(e.cfg.Root); err != nil {
		errs = append(errs, err.Error())
	}
	if _, err := state.LoadMeta(e.cfg.Root); err != nil {
		errs = append(errs, err.Error())
	}
	for _, p := range e.cfg.ProtectedPaths {
		if _, err := os.Stat(filepath.Join(e.cfg.Root, p)); err != nil {
			errs = append(errs, "守りの部分 "+p+" がない")
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d 件の問題:\n  %s", len(errs), strings.Join(errs, "\n  "))
	}
	fmt.Printf("ok (アイディア %d 件、アプリ %d 件)\n", len(all), len(e.pf.Projects))
	return nil
}

func cmdCandidates(args []string) error {
	fs := flag.NewFlagSet("candidates", flag.ContinueOnError)
	e, err := load()
	if err != nil {
		return err
	}
	n := fs.Int("n", e.cfg.Select.Candidates, "件数")
	if err := fs.Parse(args); err != nil {
		return err
	}
	all, err := ideas.Load(filepath.Join(e.cfg.Root, e.cfg.IdeasDir))
	if err != nil {
		return err
	}
	type cand struct {
		Idea, Title, Summary, Core, Wakuwaku string
	}
	var out []cand
	for _, i := range ideas.Candidates(all, e.pf.Tried(), *n, e.today()) {
		out = append(out, cand{i.Dir, i.Title, i.Sections["概要"], i.Sections["体験の核"], i.Sections["わくわくポイント"]})
	}
	return printJSON(out)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
