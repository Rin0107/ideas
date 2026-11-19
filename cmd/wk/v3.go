package main

// v3 (docs/DESIGN-v3.md) の部品: 状態を書くのは wk だけ (専用の clone)、トークンの予算、動かす時間。

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/limits"
	"github.com/Rin0107/ideas/internal/lock"
	"github.com/Rin0107/ideas/internal/state"
	"github.com/Rin0107/ideas/internal/usage"
)

// localCommands は今の worktree をそのまま見るコマンド (状態の clone に切り替えない)。
// check は CI が worktree の中身を検査する。sync は worktree のブランチを工場に入れる。guard は毎回の道具の呼び出しで動くので速くする
var localCommands = map[string]bool{"check": true, "sync": true, "guard-bash": true, "guard-edit": true}

// readOnlyCommands は状態を読むだけのコマンド。状態のロックは読む間だけ持つ (CI やビルドを長く待つ review・merge・deploy が、
// ほかの実行を止めないように)。deploy は書くときだけ e.update で取り直す (docs/DESIGN-v3.md §8)
var readOnlyCommands = map[string]bool{"due": true, "status": true, "usage": true, "candidates": true,
	"review": true, "merge": true, "deploy": true, "verify": true, "inbox": true}

// stateMode は今のコマンドが状態の clone を使うか (run が決める)。loaded は load した env (run が最後に記録を push する)。
// readOnlyCmd は読むだけのコマンドか。releaseState は状態のロックを外す (run がコマンドの終わりに呼ぶ)
var (
	stateMode    bool
	readOnlyCmd  bool
	loaded       *env
	releaseState = func() {}
)

// holdState は状態のロック (clone に同時に 1 つのコマンドしか触らない) を取る。
func holdState(cfg *config.Config) (func(), error) {
	return lock.Hold(filepath.Join(cfg.RuntimeDir, "state.lock"), stateWait, statePoll)
}

// update は、状態のロックを持たないコマンド (deploy) が書くとき、ロックを取り直して最新の状態の同じアプリに f を当てて push する。
// ロックを持っているコマンドや v2 の形では、そのまま書く
func (e *env) update(p *state.Project, f func(*state.Project)) error {
	if stateMode || e.cfg.StateCheckout == "" {
		f(p)
		return e.save()
	}
	rel, err := holdState(e.cfg)
	if err != nil {
		return err
	}
	defer rel()
	if err := openState(e.cfg); err != nil {
		return err
	}
	pf, err := state.LoadPortfolio(e.cfg.Root)
	if err != nil {
		return err
	}
	q := pf.ByRepo(p.Repo)
	if q == nil {
		return fmt.Errorf("%s が portfolio にない", p.Repo)
	}
	f(p)
	f(q)
	e.pf = pf
	if err := e.save(); err != nil {
		return err
	}
	return settleState(e.cfg.StateCheckout, "wk: "+p.Repo+" の記録")
}

// openState は状態の clone を用意して最新にする (なければ clone する)。
func openState(cfg *config.Config) error {
	dir := cfg.StateCheckout
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		url, err := gh.Run(cfg.Root, "git", "remote", "get-url", "origin")
		if err != nil {
			return fmt.Errorf("工場の origin を読めない: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return err
		}
		if _, err := gh.Run(filepath.Dir(dir), "git", "clone", "-q", strings.TrimSpace(url), dir); err != nil {
			return fmt.Errorf("工場を clone できない: %w", err)
		}
	}
	return settleState(dir, "wk: 前のコマンドの記録の残り")
}

// settleState は clone の変更を commit し、origin の main と合わせて push する。書くのは wk だけなので、ふつうは衝突しない。
func settleState(dir, msg string) error {
	out, err := gh.Run(dir, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		if _, err := gh.Run(dir, "git", "add", "-A"); err != nil {
			return err
		}
		if _, err := gh.Run(dir, "git", "commit", "-q", "-m", msg); err != nil {
			return err
		}
	}
	if _, err := gh.Run(dir, "git", "pull", "-q", "--rebase", "origin", "main"); err != nil {
		_, _ = gh.Run(dir, "git", "rebase", "--abort")
		return fmt.Errorf("工場の clone を最新にできない: %w", err)
	}
	if n, _ := gh.Run(dir, "git", "rev-list", "--count", "origin/main..HEAD"); strings.TrimSpace(n) != "0" {
		if _, err := gh.Run(dir, "git", "push", "-q", "origin", "HEAD:main"); err != nil {
			return fmt.Errorf("工場の記録を push できない (次のコマンドがもう一度試す): %w", err)
		}
	}
	return nil
}

// finishState はコマンドの終わりに記録を commit して push する。
func finishState(args []string) error {
	if !stateMode || loaded == nil || loaded.cfg.StateCheckout == "" {
		return nil
	}
	msg := commitMsg(args)
	if len([]rune(msg)) > 72 {
		msg = string([]rune(msg)[:72])
	}
	return settleState(loaded.cfg.StateCheckout, msg)
}

// commitMsg はコマンドを commit メッセージにする。パスは名前だけにする
// (ホームや一時ディレクトリのパスにはユーザー名が入り、pre-push の検査で記録の push が止まる)。
func commitMsg(args []string) string {
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = a
		if strings.Contains(a, "/") && !strings.Contains(a, " ") {
			words[i] = filepath.Base(a)
		}
	}
	return "wk " + strings.Join(words, " ")
}

// refreshWorktree は tick の worktree を工場の最新に進める (同じ実行の前の段が書いた記録を、次の段が読めるように)。
// worktree に自分の変更があれば触らない (sync で入れる)。失敗しても tick は続ける
func refreshWorktree(dir string) {
	if dir == "" {
		return
	}
	if out, err := gh.Run(dir, "git", "status", "--porcelain"); err != nil || strings.TrimSpace(out) != "" {
		return
	}
	if _, err := gh.Run(dir, "git", "fetch", "-q", "origin", "main"); err != nil {
		return
	}
	_, _ = gh.Run(dir, "git", "merge", "-q", "--ff-only", "FETCH_HEAD")
}

// --- トークン ---

// usageMatch は工場の Claude の会話の記録を見分ける (tick の worktree・claude -p の場所の名前に入る)
const usageMatch = "wakuwaku"

var claudeProjects = func() string { return config.Expand("~/.claude/projects") }

func extraUsagePath(e *env) string { return filepath.Join(e.cfg.RuntimeDir, "usage-claude-p.jsonl") }

func (e *env) usageBetween(from, to time.Time) (usage.Usage, error) {
	return usage.Sum(claudeProjects(), usageMatch, extraUsagePath(e), from, to)
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// budgetProblem は使ってよい量を超えていれば理由を返す。
// Claude の利用上限 (5 時間・週) の新しい記録があればそれで決め、なければ 1 日の予算 (max_tokens_per_day) で決める (docs/DESIGN-v3.md §9)
func budgetProblem(e *env) string {
	if r := e.limits(); r != nil {
		return limits.Problem(r, e.now, e.cfg.Limits.Policy())
	}
	if e.cfg.MaxTokensPerDay <= 0 {
		return ""
	}
	u, err := e.usageBetween(dayStart(e.now), e.now.Add(time.Minute))
	if err != nil {
		return "今日のトークンを数えられない: " + err.Error()
	}
	if u.Weighted() >= e.cfg.MaxTokensPerDay {
		return fmt.Sprintf("今日のトークンは予算に達した (%d/%d)", u.Weighted(), e.cfg.MaxTokensPerDay)
	}
	return ""
}

// limits は利用上限の新しい記録 (なければ・古ければ・読めなければ nil。そのときは 1 日の予算で止める)。
func (e *env) limits() *limits.Reading {
	if e.cfg.Limits.File == "" {
		return nil
	}
	r, err := limits.Read(e.cfg.Limits.File)
	if err != nil || r == nil || !r.Fresh(e.now, e.cfg.Limits.Policy().MaxAge) {
		return nil
	}
	return r
}

// limitsLine は利用上限の今を 1 行で返す (wk usage)。
func limitsLine(e *env) string {
	if r := e.limits(); r != nil {
		return "利用上限: " + limits.Summary(r, e.now, e.cfg.Limits.Policy())
	}
	return fmt.Sprintf("利用上限: 新しい記録がない (1 日の予算 %d で止める)", e.cfg.MaxTokensPerDay)
}

// hourProblem は今が tick を始めてよい時でなければ理由を返す。
func hourProblem(e *env) string {
	if len(e.cfg.RunHours) == 0 || slices.Contains(e.cfg.RunHours, e.now.Hour()) {
		return ""
	}
	return fmt.Sprintf("今 (%d 時) は動かす時間ではない (run_hours %v)", e.now.Hour(), e.cfg.RunHours)
}

// cmdUsage は日ごと・アプリごとのトークンと、わくわく 1 点あたりのトークンを出す。
func cmdUsage(args []string) error {
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	days := fs.Int("days", 7, "何日分の会話の記録を数えるか")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	fmt.Println(limitsLine(e))
	fmt.Println("日ごと (会話の記録から。利用上限に効く量 = トークン + キャッシュの読み出しの 1/10):")
	for i := *days - 1; i >= 0; i-- {
		from := dayStart(e.now).AddDate(0, 0, -i)
		u, err := e.usageBetween(from, from.AddDate(0, 0, 1))
		if err != nil {
			return err
		}
		fmt.Printf("  %s  %10d (トークン %d + キャッシュの読み出し %d / 10)\n", from.Format(time.DateOnly), u.Weighted(), u.Tokens, u.CacheRead)
	}
	ticks, err := state.ReadTicks(e.cfg.Root)
	if err != nil {
		return err
	}
	per := map[string]int{}
	for _, t := range ticks {
		if t.Repo != "" {
			per[t.Repo] += usage.Usage{Tokens: t.Tokens, CacheRead: t.CacheRead}.Weighted()
		}
	}
	fmt.Println("アプリごと (tick の記録から。v3 より前の tick は数えていない):")
	repos := make([]string, 0, len(per))
	for r := range per {
		repos = append(repos, r)
	}
	sort.Strings(repos)
	for _, r := range repos {
		line := fmt.Sprintf("  %s  %d", r, per[r])
		if w := lastWakuwaku(e, r); w > 0 && per[r] > 0 {
			line += fmt.Sprintf("  わくわく %d → 1 点あたり %d", w, per[r]/w)
		}
		fmt.Println(line)
	}
	return nil
}

// lastWakuwaku はアプリの直近の評価のわくわく (なければ 0)。evaluate の記録の note「わくわく N」から読む
func lastWakuwaku(e *env, repo string) int {
	for _, p := range e.pf.Projects {
		if p.Repo != repo {
			continue
		}
		w := 0
		for _, ev := range p.History {
			var n int
			if i := strings.Index(ev.Note, "わくわく "); ev.Event == "evaluated" && i >= 0 {
				if _, err := fmt.Sscanf(ev.Note[i+len("わくわく "):], "%d", &n); err == nil {
					w = n
				}
			}
		}
		return w
	}
	return 0
}

// factoryMain は工場の正本 (main) の checkout。v3 は wk の clone、v2 は手元の main。
// 守りのレビューの指示や個人情報の検査など、検査される側の worktree から読んではいけないものに使う
func (e *env) factoryMain() string {
	if e.cfg.StateCheckout != "" {
		return e.cfg.StateCheckout
	}
	return e.cfg.MainCheckout
}
