package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/guard"
	"github.com/Rin0107/ideas/internal/ideas"
	"github.com/Rin0107/ideas/internal/lock"
	"github.com/Rin0107/ideas/internal/next"
	"github.com/Rin0107/ideas/internal/role"
	"github.com/Rin0107/ideas/internal/room"
	"github.com/Rin0107/ideas/internal/state"
)

// env は 1 回のコマンドで使う設定と状態。
type env struct {
	cfg *config.Config
	pf  *state.Portfolio
	now time.Time
	// work は呼ばれた worktree (工場の中から呼ばれたとき)。状態は cfg.Root (v3 では wk だけが書く clone) にある
	work string
}

func load() (*env, error) {
	wd, _ := os.Getwd()
	work := ""
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
	} else {
		work = cfg.Root
	}
	// v3: 状態と記録は wk だけが書く clone に置く (worktree・手元の main からは書かない。docs/DESIGN-v3.md §2)
	// v3.1: 実行が何本も同時に動くので、clone に触るあいだは状態のロックを持つ (読むだけのコマンドは読む間だけ)
	if stateMode && cfg.StateCheckout != "" {
		releaseState() // 同じプロセスで前に持ったもの (テストで何度も load する)
		rel, err := holdState(cfg)
		if err != nil {
			return nil, err
		}
		if err := openState(cfg); err != nil {
			rel()
			return nil, err
		}
		if cfg, err = config.Load(cfg.StateCheckout); err != nil {
			rel()
			return nil, err
		}
		releaseState = rel
		if readOnlyCmd {
			releaseState, stateMode = func() {}, false
			rel()
		}
	}
	pf, err := state.LoadPortfolio(cfg.Root)
	if err != nil {
		return nil, err
	}
	loaded = &env{cfg: cfg, pf: pf, now: time.Now(), work: work}
	return loaded, nil
}

func (e *env) factoryRepo() string { return e.cfg.Owner + "/" + filepath.Base(e.cfg.MainCheckout) }
func (e *env) today() string       { return e.now.Format(time.DateOnly) }
func (e *env) ttl() time.Duration  { return time.Duration(e.cfg.LockTTLMinutes) * time.Minute }

// tickOwner は tick の持ち主のプロセス (wk を呼んだ claude) と開始時刻。それが終わればロックは古い (lock.Info.Stale)。
// 2026-11-24 に 03:00 の tick が wk done なしで落ち、TTL (4 時間) まで次の tick がそのアプリを選べなかった。テストで差し替える
var tickOwner = func() (int, string) { return lock.Owner(os.Getppid(), "claude") }

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
	s := next.Snapshot{Now: e.now, Cfg: e.cfg, Meta: meta, Untried: ideas.Untried(all, e.pf.Tried()),
		FactoryRepo: e.factoryRepo(), Claimed: map[string]bool{}, FocusReady: focusTemplateReady(e)}
	if s.FactoryFixes, err = gh.LabeledIssues(s.FactoryRepo, next.LabelFix); err != nil {
		return next.Snapshot{}, err
	}
	for _, p := range e.pf.Actives() {
		ap := next.App{Project: p}
		if p.Repo != "" && (p.Status == state.Building || p.Status == state.Iterating) {
			if ap.Issues, err = gh.OpenIssues(p.Repo, p.Milestone); err != nil {
				return next.Snapshot{}, err
			}
			proposals, err := gh.OpenWithLabel(p.Repo, labelProposal)
			if err != nil {
				return next.Snapshot{}, err
			}
			ap.Proposals = len(proposals)
			if e.cfg.V4.Enabled {
				if ap.Tasks, err = state.LoadTasks(e.cfg.Root, p.Repo); err != nil {
					return next.Snapshot{}, err
				}
			}
		}
		s.Apps = append(s.Apps, ap)
	}
	// ほかの実行が進めているもの (v3.1、docs/DESIGN-v3.md §8)
	others, err := e.otherRuns()
	if err != nil {
		return next.Snapshot{}, err
	}
	for _, l := range others {
		for _, k := range []string{l.Repo, l.Idea} {
			if k != "" {
				s.Claimed[k] = true
			}
		}
		if next.Global[l.Phase] {
			s.Claimed[l.Phase] = true
		}
	}
	return s, nil
}

// runKey は今の実行の名前 (tick の worktree の名前)。ロックと今の段の記録は実行ごとに分ける
func (e *env) runKey() string {
	d := e.work
	if d == "" {
		d, _ = os.Getwd()
	}
	return filepath.Base(d)
}

// otherRuns は、ほかの実行の生きているロック (key → 中身)。
func (e *env) otherRuns() (map[string]*lock.Info, error) { return e.liveRuns(e.runKey()) }

// liveRuns は生きているロック (key → 中身)。skip の実行は除く
func (e *env) liveRuns(skip string) (map[string]*lock.Info, error) {
	all, err := lock.List(e.cfg.RuntimeDir)
	if err != nil {
		return nil, err
	}
	for k, l := range all {
		if k == skip || l.Stale(e.now, e.ttl()) {
			delete(all, k)
		}
	}
	return all, nil
}

// reapStale は落ちた実行のロックを外し、aborted と記録する (ロックを引き継げば直るので、失敗には数えない。自分で直せること)。
// state は公開されるので、家のディレクトリ (ユーザー名) が入る絶対パスではなく worktree の名前だけを書く (issue #5)
func (e *env) reapStale() error {
	all, err := lock.List(e.cfg.RuntimeDir)
	if err != nil {
		return err
	}
	for k, l := range all {
		if k != e.runKey() && l.Stale(e.now, e.ttl()) {
			_ = state.AppendTick(e.cfg.Root, state.Tick{Start: l.Since, End: e.now, Phase: l.Phase, Repo: l.Repo, Result: "aborted",
				Note: "ロックが " + filepath.Base(l.Worktree) + " に残っていた"})
			_ = lock.Release(e.cfg.RuntimeDir, k)
			_ = os.Remove(e.currentPath(k))
		}
	}
	return nil
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
	if why := hourProblem(e); why != "" {
		return exitError{1, why}
	}
	rt, err := state.LoadRuntime(e.cfg.RuntimeDir, e.now)
	if err != nil {
		return err
	}
	if rt.TicksToday >= e.cfg.TicksPerDay {
		return exitError{1, fmt.Sprintf("今日の tick は上限 (%d/%d)", rt.TicksToday, e.cfg.TicksPerDay)}
	}
	if why := budgetProblem(e); why != "" {
		return exitError{1, why}
	}
	// 同時に動く実行は max_parallel 本まで (v3.1)。ロックは実行ごと。due は始める前 (Orca の precheck) なので、全部を数える
	others, err := e.liveRuns("")
	if err != nil {
		return err
	}
	if len(others) >= e.cfg.Parallel() {
		var now []string
		for _, l := range others {
			now = append(now, fmt.Sprintf("%s %s から", l.Phase, l.Since.Format(time.TimeOnly)))
		}
		slices.Sort(now)
		return exitError{1, fmt.Sprintf("同時に動ける実行は %d 本まで (今: %s)", e.cfg.Parallel(), strings.Join(now, "・"))}
	}
	fmt.Printf("ok (今日 %d/%d tick、ほかの実行 %d/%d 本)\n", rt.TicksToday, e.cfg.TicksPerDay, len(others), e.cfg.Parallel())
	return nil
}

func cmdNext(args []string) error {
	fs := flag.NewFlagSet("next", flag.ContinueOnError)
	planner := fs.Bool("planner", false, "計画の実行 (v4): 評価・計画・修理をやる。なければ tick の実行 (task だけ)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	if p := e.stopFile(); p != "" {
		return errors.New("STOP がある (" + p + ")")
	}
	wd, _ := os.Getwd()
	rt, err := state.LoadRuntime(e.cfg.RuntimeDir, e.now)
	if err != nil {
		return err
	}
	// leader は 1 回の実行で何段か続けて進める (docs/ROLES.md)。進めてよいかは毎回ここで決める。
	// だめなら idle を返す (エラーではない。leader はそこで終わる)
	if why := stepLimit(e, rt, wd); why != "" {
		return printJSON(next.Action{Phase: state.PhaseIdle, Role: role.Leader, Reason: why})
	}
	if err := e.reapStale(); err != nil {
		return err
	}
	s, err := e.snapshot()
	if err != nil {
		return err
	}
	s.Planner = *planner
	if run := rt.Current(wd, e.now, e.ttl()); run != nil {
		s.Prefer = run.Repo
	}
	a := next.Decide(s)
	key := e.runKey()
	pid, started := tickOwner()
	stale, err := lock.Acquire(e.cfg.RuntimeDir, key, lock.Info{Since: e.now, Phase: a.Phase, Worktree: wd, Repo: a.Repo, Idea: a.Idea,
		Pid: pid, Started: started}, e.ttl())
	if err != nil {
		return err
	}
	if stale != nil {
		// この実行の前の段が終わらずに消えた
		_ = state.AppendTick(e.cfg.Root, state.Tick{Start: stale.Since, End: e.now, Phase: stale.Phase, Repo: stale.Repo, Result: "aborted",
			Note: "ロックが " + filepath.Base(stale.Worktree) + " に残っていた"})
	}
	if a.Phase == state.PhaseIdle {
		_ = lock.Release(e.cfg.RuntimeDir, key)
		return printJSON(a)
	}
	rt.TicksToday++
	rt.Step(wd, e.now, e.ttl()).Repo = a.Repo
	if err := state.SaveRuntime(e.cfg.RuntimeDir, rt); err != nil {
		return err
	}
	if p := e.pf.ByRepo(a.Repo); p != nil && p.Active() {
		p.Ticks.Total++
		// 作る・見回る・仕分けるは、この段階の tick を使う (だから時間切れが必ず来る。spec/Factory.tla)
		if a.Phase == state.PhaseBuild || a.Phase == state.PhasePatrol || a.Phase == state.PhaseTriage {
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

// stepLimit は、いま次の段に進んではいけない理由を返す (空なら進んでよい)。
func stepLimit(e *env, rt *state.Runtime, wd string) string {
	if why := room.Problems(sysRoot, runtime.NumCPU(), room.Default()); len(why) > 0 {
		return "機械に余裕がない: " + strings.Join(why, " / ")
	}
	if rt.TicksToday >= e.cfg.TicksPerDay {
		return fmt.Sprintf("今日の tick は上限 (%d/%d)", rt.TicksToday, e.cfg.TicksPerDay)
	}
	if run := rt.Current(wd, e.now, e.ttl()); e.cfg.StepsPerRun > 0 && run != nil && run.Steps >= e.cfg.StepsPerRun {
		return fmt.Sprintf("この実行で進める段の上限 (%d/%d)", run.Steps, e.cfg.StepsPerRun)
	}
	return budgetProblem(e)
}

// 今の tick の段階を runtime_dir に残す (done が読む)
func writeCurrent(e *env, a next.Action) error {
	b, _ := json.Marshal(struct {
		next.Action
		Start time.Time `json:"start"`
	}{a, e.now})
	return os.WriteFile(e.currentPath(e.runKey()), b, 0o644)
}

// currentPath は実行 key の今の段の記録。
func (e *env) currentPath(key string) string {
	return filepath.Join(e.cfg.RuntimeDir, "current-"+key+".json")
}

// bindCurrent は今の段に、決まったアプリを書き足す (select でアイディアが決まったとき)。ほかの実行はそれを選ばない。
// tick の外 (今の段がない) なら何もしない
func bindCurrent(e *env, repo, idea string) error {
	a, start, err := readCurrent(e)
	if err != nil {
		return nil
	}
	a.Repo, a.Idea = repo, idea
	b, _ := json.Marshal(struct {
		next.Action
		Start time.Time `json:"start"`
	}{a, start})
	if err := os.WriteFile(e.currentPath(e.runKey()), b, 0o644); err != nil {
		return err
	}
	return lock.Claim(e.cfg.RuntimeDir, e.runKey(), repo, idea)
}

func readCurrent(e *env) (next.Action, time.Time, error) {
	var c struct {
		next.Action
		Start time.Time `json:"start"`
	}
	b, err := os.ReadFile(e.currentPath(e.runKey()))
	if err != nil {
		return next.Action{}, time.Time{}, errors.New("今の tick がない (wk next を先に実行する)")
	}
	err = json.Unmarshal(b, &c)
	return c.Action, c.Start, err
}

func cmdDone(args []string) error {
	fs := flag.NewFlagSet("done", flag.ContinueOnError)
	result := fs.String("result", "", "ok / retry / fix / human (docs/DESIGN.md §9)")
	note := fs.String("note", "", "1 行のメモ (fix と human では issue の題になる)")
	detail := fs.String("detail", "", "fix・human: 本文のファイル (何が起きたか・どう直すか・人にやってほしい手順と差分)")
	in := fs.String("in", "", "fix: factory なら工場 repo に立てる (既定はその tick の repo)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !slices.Contains([]string{"ok", "retry", "fix", "human"}, *result) {
		return errors.New("--result は ok・retry・fix・human のどれか")
	}
	body, err := readDetail(*detail)
	if err != nil {
		return err
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
		if a.Phase == state.PhaseIntake || a.Phase == state.PhaseRetro || a.Phase == state.PhasePatrol {
			m, err := state.LoadMeta(e.cfg.Root)
			if err != nil {
				return err
			}
			if a.Phase == state.PhaseIntake {
				m.LastIntake = e.today()
			}
			if a.Phase == state.PhaseRetro {
				m.LastRetro = e.today()
			}
			if a.Phase == state.PhasePatrol {
				if m.LastPatrol == nil {
					m.LastPatrol = map[string]string{}
				}
				m.LastPatrol[a.Role] = e.today()
			}
			if err := state.SaveMeta(e.cfg.Root, m); err != nil {
				return err
			}
		}
	}
	// この段で使ったトークン (v3 の予算と、わくわく 1 点あたりの材料)。数えられなくても記録は残す
	u, err := e.usageBetween(start, e.now.Add(time.Second))
	if err != nil {
		fmt.Fprintln(os.Stderr, "トークンを数えられない:", err)
	}
	if err := state.AppendTick(e.cfg.Root, state.Tick{Start: start, End: e.now, Phase: a.Phase, Repo: a.Repo,
		Issue: a.Issue, Role: a.Role, Result: *result, Note: *note, Tokens: u.Tokens, CacheRead: u.CacheRead}); err != nil {
		return err
	}
	// 人の手が要るものだけを失敗に数える。gh が落ちても tick は終える (ロックを残さない)
	if err := settle(e, rt, a, *result, *note, body, *in); err != nil {
		fmt.Fprintln(os.Stderr, "issue の作成に失敗:", err)
	}
	if err := state.SaveRuntime(e.cfg.RuntimeDir, rt); err != nil {
		return err
	}
	if rt.ConsecutiveFailures >= e.cfg.MaxConsecutiveFailures {
		if err := stopWithIncident(e, rt, a, *note); err != nil {
			fmt.Fprintln(os.Stderr, "incident の作成に失敗:", err)
		}
	}
	_ = os.Remove(e.currentPath(e.runKey()))
	if err := lock.Release(e.cfg.RuntimeDir, e.runKey()); err != nil {
		return err
	}
	fmt.Printf("%s %s (連続失敗 %d/%d)\n", a.Phase, *result, rt.ConsecutiveFailures, e.cfg.MaxConsecutiveFailures)
	return nil
}

// settle は fix と human の後始末 (直す issue・人宛ての issue) をし、失敗の数を数える。
func settle(e *env, rt *state.Runtime, a next.Action, result, note, body, in string) error {
	switch result {
	case "human":
		rt.ConsecutiveFailures++
		return handleHuman(e, a, note, body)
	case "fix":
		up, err := handleFix(e, a, note, body, in)
		if err != nil || !up {
			return err
		}
		rt.ConsecutiveFailures++
		return handleHuman(e, a, fmt.Sprintf("直す issue %s #%d が %d 回で直らない", a.Repo, a.Issue, e.cfg.MaxAttemptsPerIssue),
			note+"\n\n"+body)
	}
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
	if err := ensureLabel(e.factoryRepo(), labelHuman); err != nil {
		return err
	}
	_, err := gh.GH("issue", "create", "--repo", e.factoryRepo(), "--title", "wk:incident 連続失敗で停止 "+e.today(),
		"--label", "wk:incident", "--label", labelHuman, "--body", body)
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
	main, merge := e.cfg.MainCheckout, []string{"merge", "--no-edit", br}
	if e.cfg.StateCheckout != "" {
		// v3: 手元の main ではなく、wk だけが書く clone に入れる (手元の main が汚れていても止まらない)
		rel, err := holdState(e.cfg)
		if err != nil {
			return err
		}
		defer rel()
		if err := openState(e.cfg); err != nil {
			return err
		}
		if _, err := gh.Run(wd, "git", "fetch", "-q", "origin", "main"); err != nil {
			return err
		}
		// clone は別の repo なので、worktree のブランチを直接取り込む
		main, merge = e.cfg.StateCheckout, []string{"pull", "-q", "--no-rebase", "--no-edit", wd, br}
	} else {
		if out, _ := gh.Run(main, "git", "status", "--porcelain", "--untracked-files=no"); strings.TrimSpace(out) != "" {
			return errors.New("工場の main に未コミットの変更がある: " + main)
		}
		if _, err := gh.Run(main, "git", "pull", "--ff-only", "origin", "main"); err != nil {
			return err
		}
	}
	if out, err := gh.Run(wd, "git", "diff", "--name-only", "origin/main...HEAD"); err != nil {
		return err
	} else if hit := guard.TouchesProtected(strings.Split(out, "\n"), e.cfg.HardProtected()); len(hit) > 0 {
		return fmt.Errorf("工場の力を広げる守りの部分が変わっているので main に入れない (人だけが変える): %s", strings.Join(hit, ", "))
	} else if soft := guard.TouchesProtected(strings.Split(out, "\n"), e.cfg.ReviewablePaths); len(soft) > 0 {
		if err := guardianSync(e, wd, soft); err != nil {
			return err
		}
	}
	if _, err := gh.Run(main, "git", merge...); err != nil {
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
	act := e.pf.Actives()
	if short {
		var parts []string
		for _, p := range act {
			parts = append(parts, fmt.Sprintf("%s を %s (%d tick)", p.Idea, p.Status, p.Ticks.Total))
		}
		// switch の case の条件はミューテーションの道具がカバレッジの外と数えるので、if で書く
		if stop != "" {
			b, _ := os.ReadFile(stop)
			fmt.Printf("wakuwaku: 停止中 — %s", b)
			return nil
		}
		if len(act) > 0 {
			fmt.Printf("wakuwaku: %s\n", strings.Join(parts, "・"))
			return nil
		}
		fmt.Println("wakuwaku: 作っているものはない")
		return nil
	}
	fmt.Printf("工場: %s\n停止: %v\n今日の tick: %d/%d (連続失敗 %d)\n", e.cfg.Root, stop != "", rt.TicksToday, e.cfg.TicksPerDay, rt.ConsecutiveFailures)
	if runs, err := lock.List(e.cfg.RuntimeDir); err == nil {
		fmt.Printf("動いている実行: %d 本 (同時に %d 本まで、作りかけは %d 件まで)\n", len(runs), e.cfg.Parallel(), e.cfg.WIP())
		for _, k := range slices.Sorted(maps.Keys(runs)) {
			l := runs[k]
			fmt.Printf("  %s: %s %s%s (%s から)\n", k, l.Phase, l.Repo, l.Idea, l.Since.Format(time.TimeOnly))
		}
	}
	for _, p := range act {
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
		fmt.Printf("次の段階: %s (%s) %s #%d — %s\n", a.Phase, a.Role, a.Repo, a.Issue, a.Reason)
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
	errs = append(errs, e.pf.Validate(e.cfg.WIP())...)
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
	for _, p := range e.cfg.ReviewablePaths {
		if !slices.Contains(e.cfg.ProtectedPaths, p) {
			errs = append(errs, "reviewable_paths の "+p+" が protected_paths にない")
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
	cands := ideas.Candidates(all, e.pf.Tried(), *n, e.today())
	if e.cfg.Focus != "" { // 主力のアプリを始めるときは、主力のアイディアだけ (docs/DESIGN-v3.md §10)
		cands = slices.DeleteFunc(all, notFocus(e.cfg.Focus))
	}
	for _, i := range cands {
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
