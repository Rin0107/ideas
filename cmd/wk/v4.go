package main

// v4 (docs/DESIGN-v4.md): 計画と実行を分ける。
//   - 計画 (毎日 1 回、Opus): wk inbox で人の issue を読み、wk plan で task の issue と完了条件のテストを作る
//   - 実行 (tick、Sonnet): task を 1 件ずつ、wk verify が通るまでやる。merge は wk verify が通ったコミットだけ

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/guard"
	"github.com/Rin0107/ideas/internal/next"
	"github.com/Rin0107/ideas/internal/role"
	"github.com/Rin0107/ideas/internal/state"
	"github.com/Rin0107/ideas/internal/verdict"
)

// ---- inbox: 人が書いた issue ----

// InboxIssue は持ち主が工場に頼んだこと (wk:・role: のラベルのない、open な issue)。計画が task にして閉じる
type InboxIssue struct {
	Ref    string `json:"ref"` // owner/repo#n
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

func isFactoryLabel(l string) bool {
	return strings.HasPrefix(l, "wk:") || strings.HasPrefix(l, "role:")
}

// inbox は工場の repo と作りかけのアプリの repo から、持ち主の issue を集める。
func inbox(e *env) ([]InboxIssue, error) {
	repos := []string{e.factoryRepo()}
	for _, p := range e.pf.Actives() {
		if p.Repo != "" && !slices.Contains(repos, p.Repo) {
			repos = append(repos, p.Repo)
		}
	}
	out := []InboxIssue{}
	for _, r := range repos {
		s, err := gh.GH("issue", "list", "--repo", r, "--author", e.cfg.Owner, "--state", "open", "--limit", "100",
			"--json", "number,title,body,labels")
		if err != nil {
			return nil, err
		}
		var raw []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Body   string `json:"body"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		}
		if err := json.Unmarshal([]byte(s), &raw); err != nil {
			return nil, fmt.Errorf("%s の issue を読めない: %w", r, err)
		}
		for _, is := range raw {
			if !slices.ContainsFunc(is.Labels, func(l struct {
				Name string `json:"name"`
			}) bool {
				return isFactoryLabel(l.Name)
			}) {
				out = append(out, InboxIssue{Ref: fmt.Sprintf("%s#%d", r, is.Number), Repo: r, Number: is.Number, Title: is.Title, Body: is.Body})
			}
		}
	}
	return out, nil
}

func cmdInbox(args []string) error {
	e, err := load()
	if err != nil {
		return err
	}
	in, err := inbox(e)
	if err != nil {
		return err
	}
	return printJSON(in)
}

// ---- plan: 計画を issue と完了条件のテストにする ----

// V4Plan は計画の実行 (Opus) が書く。wk が形を検査し、テストが今は通らないことを確かめてから登録する
type V4Plan struct {
	Goal     string     `json:"goal"`
	Setup    string     `json:"setup,omitempty"` // 完了条件の前に 1 回 (npm ci など)
	Tasks    []V4Task   `json:"tasks"`
	Declined []Declined `json:"declined,omitempty"` // task にしない人の issue と、その理由
}

type V4Task struct {
	Title   string        `json:"title"`
	Purpose string        `json:"purpose"` // 目的 (1〜2 文)
	Steps   []string      `json:"steps"`   // やること (考えずにできる粒度)
	Checks  []state.Check `json:"checks"`  // 完了条件 (終了コード 0 で満たす)
	Tests   []string      `json:"tests"`   // 計画が書いた受け入れテスト (--dir からの相対)
	Paths   []string      `json:"paths"`   // tick が変えてよいファイル
	After   []int         `json:"after,omitempty"`
	From    []string      `json:"from,omitempty"` // 元の人の issue (owner/repo#n)
	Role    string        `json:"role,omitempty"`
}

type Declined struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

func cleanRel(p string) bool {
	return p != "" && !filepath.IsAbs(p) && filepath.Clean(p) == p && p != "." && !strings.HasPrefix(p, "../") && p != ".."
}

// validate は計画の形を検査する (GitHub に何かを作る前)。in は今の人の issue
func (pl V4Plan) validate(min int, in []InboxIssue, gc guard.Context) error {
	var errs []string
	if strings.TrimSpace(pl.Goal) == "" {
		errs = append(errs, "goal が空")
	}
	if len(pl.Tasks) < min {
		errs = append(errs, fmt.Sprintf("task は %d 件以上 (今 %d 件)", min, len(pl.Tasks)))
	}
	refs := map[string]bool{}
	for _, is := range in {
		refs[is.Ref] = false
	}
	cmds := []string{pl.Setup}
	for i, t := range pl.Tasks {
		at := fmt.Sprintf("tasks[%d]", i)
		if strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.Purpose) == "" {
			errs = append(errs, at+": title と purpose が要る")
		}
		if len(t.Steps) == 0 {
			errs = append(errs, at+": steps が空")
		}
		if len(t.Checks) == 0 {
			errs = append(errs, at+": checks が空 (完了条件を検査できるコマンドにする)")
		}
		for k, c := range t.Checks {
			if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Run) == "" {
				errs = append(errs, fmt.Sprintf("%s.checks[%d]: name と run が要る", at, k))
			}
			cmds = append(cmds, c.Run)
		}
		if len(t.Tests) == 0 {
			errs = append(errs, at+": tests が空 (受け入れテストを先に書く)")
		}
		if len(t.Paths) == 0 {
			errs = append(errs, at+": paths が空")
		}
		for _, p := range append(slices.Clone(t.Tests), t.Paths...) {
			if !cleanRel(p) {
				errs = append(errs, fmt.Sprintf("%s: パス %q は repo の中の相対パスにする", at, p))
			}
		}
		if hit := guard.TouchesProtected(t.Tests, appHard); len(hit) > 0 {
			errs = append(errs, fmt.Sprintf("%s: テストを守りの部分に置かない: %s", at, strings.Join(hit, ", ")))
		}
		for _, a := range t.After {
			if a < 0 || a >= i {
				errs = append(errs, fmt.Sprintf("%s: after の %d は前の task の添字にする", at, a))
			}
		}
		if t.Role != "" && !role.Valid(t.Role) {
			errs = append(errs, fmt.Sprintf("%s: role %q は不正", at, t.Role))
		}
		for _, r := range t.From {
			if _, ok := refs[r]; !ok {
				errs = append(errs, fmt.Sprintf("%s: from の %s は人の issue (wk inbox) にない", at, r))
			}
			refs[r] = true
		}
	}
	for _, d := range pl.Declined {
		if _, ok := refs[d.Ref]; !ok {
			errs = append(errs, fmt.Sprintf("declined の %s は人の issue (wk inbox) にない", d.Ref))
		}
		if strings.TrimSpace(d.Reason) == "" {
			errs = append(errs, "declined の "+d.Ref+": reason が空")
		}
		refs[d.Ref] = true
	}
	for _, is := range in {
		if !refs[is.Ref] {
			errs = append(errs, is.Ref+" (人の issue) を task にするか declined に書く")
		}
	}
	for _, c := range cmds {
		if why := guard.Bash(c, gc); c != "" && why != "" {
			errs = append(errs, fmt.Sprintf("コマンド %q: %s", c, why))
		}
	}
	if len(errs) > 0 {
		return errors.New("計画の形が不正:\n  " + strings.Join(errs, "\n  "))
	}
	return nil
}

func taskBody(t V4Task, after []int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## 目的\n%s\n\n## やること\n", strings.TrimSpace(t.Purpose))
	for i, s := range t.Steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(s))
	}
	b.WriteString("\n## 完了条件\n`wk verify` が次を全部実行し、どれも終了コード 0 なら完了 (wk が判定する。テストと検査は変えない)。\n\n")
	for _, c := range t.Checks {
		fmt.Fprintf(&b, "- [ ] %s: `%s`\n", c.Name, c.Run)
	}
	b.WriteString("\n## 受け入れテスト (変えない)\n")
	for _, f := range t.Tests {
		fmt.Fprintf(&b, "- `%s`\n", f)
	}
	b.WriteString("\n## 変えてよいファイル\n")
	for _, f := range t.Paths {
		fmt.Fprintf(&b, "- `%s`\n", f)
	}
	b.WriteString("\n## ブランチ\n`wk/t<この issue の番号>` (受け入れテストが入っている)。この上で作る\n")
	if len(after) > 0 {
		b.WriteString("\n## 先に終わっている task\n")
		for _, n := range after {
			fmt.Fprintf(&b, "- #%d\n", n)
		}
	}
	if len(t.From) > 0 {
		b.WriteString("\n## 元の issue\n")
		for _, r := range t.From {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	return b.String()
}

// runShell は完了条件のコマンドを dir で実行する (鍵らしい環境変数は渡さない)。出力の末尾を返す
func runShell(dir, script string, timeout time.Duration) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Dir = dir
	cmd.Env = scrubEnv(os.Environ())
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return false, tail(string(out), 20) + "\n(時間切れ " + timeout.String() + ")"
	}
	return err == nil, tail(string(out), 20)
}

func fileSum(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

var issueURL = regexp.MustCompile(`/issues/(\d+)\s*$`)

// git は v4 の git の呼び出し (テストで途中の失敗を起こせるように変数にする)
var git = func(dir string, args ...string) (string, error) { return gh.Run(dir, "git", args...) }

func cmdPlan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	planPath := fs.String("plan", "", "計画の JSON")
	dirFlag := fs.String("dir", ".", "受け入れテストを書いたアプリの worktree")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	if !e.cfg.V4.Enabled {
		return errors.New("v4 が有効でない (factory.json の v4.enabled)。v3 は wk iterate")
	}
	p, err := e.project()
	if err != nil {
		return err
	}
	if p.Verdict != "iterate" && !(p.Status == state.Building && p.Iteration == 0) {
		return fmt.Errorf("計画できるのは評価が iterate のときか、最初の版 (今 %s / %q)", p.Status, p.Verdict)
	}
	var pl V4Plan
	if err := verdict.ReadJSON(*planPath, &pl); err != nil {
		return err
	}
	in, err := inbox(e)
	if err != nil {
		return err
	}
	gc := guard.Context{Owner: e.cfg.Owner, Factory: e.factoryRepo(), Owned: e.pf.Repos(), Protected: appHard}
	if err := pl.validate(e.cfg.V4.Min(), in, gc); err != nil {
		return err
	}
	dir, _ := filepath.Abs(*dirFlag) // 今の場所が読めれば失敗しない
	// 1. テストのブランチを手元に作り、完了条件が今は通らないことを確かめる (最初から通る task は作る意味がない)
	app := filepath.Join(e.cfg.AppsDir, filepath.Base(p.Repo))
	wt := filepath.Join(e.cfg.RuntimeDir, "plan", filepath.Base(p.Repo))
	if _, err := git(app, "fetch", "-q", "origin"); err != nil {
		return err
	}
	_, _ = git(app, "worktree", "remove", "--force", wt)
	_ = os.RemoveAll(wt)
	if _, err := git(app, "worktree", "add", "-q", "--detach", wt, "origin/main"); err != nil {
		return err
	}
	defer func() { _, _ = git(app, "worktree", "remove", "--force", wt) }()
	sums := make([]map[string]string, len(pl.Tasks))
	var errs []string
	for i, t := range pl.Tasks {
		if _, err := git(wt, "checkout", "-q", "-B", fmt.Sprintf("wk/plan-%d", i), "origin/main"); err != nil {
			return err
		}
		sums[i] = map[string]string{}
		for _, f := range t.Tests {
			b, err := os.ReadFile(filepath.Join(dir, f))
			if err != nil {
				return fmt.Errorf("tasks[%d] のテスト %s: %w", i, f, err)
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(wt, f)), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(wt, f), b, 0o644); err != nil {
				return err
			}
			s := sha256.Sum256(b)
			sums[i][f] = hex.EncodeToString(s[:])
		}
		if _, err := git(wt, append([]string{"add", "--"}, t.Tests...)...); err != nil {
			return err
		}
		if _, err := git(wt, "commit", "-q", "--allow-empty", "-m", "task: "+t.Title+" の受け入れテスト"); err != nil {
			return err
		}
		if i == 0 && pl.Setup != "" {
			if ok, out := runShell(wt, pl.Setup, e.cfg.V4.CheckTimeout()); !ok {
				return fmt.Errorf("setup (%s) が落ちる:\n%s", pl.Setup, out)
			}
		}
		failing := 0
		for _, c := range t.Checks {
			if ok, _ := runShell(wt, c.Run, e.cfg.V4.CheckTimeout()); !ok {
				failing++
			}
		}
		if failing == 0 {
			errs = append(errs, fmt.Sprintf("tasks[%d] (%s): 完了条件が今もう全部通る (まだ通らないテストを書く)", i, t.Title))
		}
	}
	if len(errs) > 0 {
		return errors.New("計画を登録しない:\n  " + strings.Join(errs, "\n  "))
	}
	// 2. マイルストーンと issue を作り、テストのブランチを push する
	old := p.Milestone
	ms := fmt.Sprintf("v0.%d", p.Iteration+2)
	if err := createMilestone(p.Repo, ms, pl.Goal); err != nil {
		return err
	}
	tasks, err := state.LoadTasks(e.cfg.Root, p.Repo)
	if err != nil {
		return err
	}
	nums := make([]int, len(pl.Tasks))
	for i, t := range pl.Tasks {
		var after []int
		for _, a := range t.After {
			after = append(after, nums[a])
		}
		out, err := gh.GH("issue", "create", "--repo", p.Repo, "--title", t.Title, "--body", taskBody(t, after),
			"--milestone", ms, "--label", "wk:task", "--label", role.Label(roleOr(t.Role, role.Engineer)))
		if err != nil {
			return err
		}
		m := issueURL.FindStringSubmatch(strings.TrimSpace(out))
		if m == nil {
			return fmt.Errorf("作った issue の番号を読めない: %q", out)
		}
		nums[i], _ = strconv.Atoi(m[1])
		branch := fmt.Sprintf("wk/t%d", nums[i])
		if _, err := git(wt, "push", "-q", "-f", "origin", fmt.Sprintf("wk/plan-%d:refs/heads/%s", i, branch)); err != nil {
			return err
		}
		tasks.Put(&state.Task{Issue: nums[i], Title: t.Title, Branch: branch, Tests: sums[i], Paths: t.Paths, Setup: pl.Setup,
			Checks: t.Checks, After: after, From: t.From, Plan: e.today()})
	}
	if err := state.SaveTasks(e.cfg.Root, p.Repo, tasks); err != nil {
		return err
	}
	// 3. 前の計画の残りは閉じる (この計画が作り直した。ブロックの理由は issue に残っている)。
	// 作っていないので wk:task を外し「見送り」で閉じる。外さないと CI の ac-coverage が「閉じた task」として
	// その受け入れ条件のテストを求め、新しい計画の task がどれも main に入らない (2026-11-28 夜ふかし星座)
	if old != "" && old != ms {
		rest, err := gh.OpenIssues(p.Repo, old)
		if err != nil {
			return err
		}
		for _, is := range rest {
			if _, err := gh.GH("issue", "edit", fmt.Sprint(is.Number), "--repo", p.Repo, "--remove-label", "wk:task"); err != nil {
				return err
			}
			if _, err := gh.GH("issue", "close", fmt.Sprint(is.Number), "--repo", p.Repo, "--reason", "not planned",
				"--comment", "wakuwaku factory: "+ms+" の計画で作り直した"); err != nil {
				return err
			}
		}
	}
	// 4. 人の issue を閉じる (どの task にしたか・しなかった理由を残す)
	made := map[string][]string{}
	for i, t := range pl.Tasks {
		for _, r := range t.From {
			made[r] = append(made[r], fmt.Sprintf("%s#%d", p.Repo, nums[i]))
		}
	}
	for _, is := range in {
		msg := "wakuwaku factory: task にした: " + strings.Join(made[is.Ref], " ")
		for _, d := range pl.Declined {
			if d.Ref == is.Ref {
				msg = "wakuwaku factory: task にしない: " + d.Reason
			}
		}
		if _, err := gh.GH("issue", "close", fmt.Sprint(is.Number), "--repo", is.Repo, "--comment", msg); err != nil {
			return err
		}
	}
	p.Iteration++
	p.Milestone, p.Status, p.Verdict, p.Ticks.Phase, p.Planned = ms, state.Iterating, "", 0, state.PlanNight(e.now)
	e.event(p, "planned", fmt.Sprintf("%s: %s (task %d 件)", ms, pl.Goal, len(pl.Tasks)))
	if err := e.appendIdeaHistory(p.Idea, fmt.Sprintf("計画 %s: %s (task %d 件)", ms, pl.Goal, len(pl.Tasks))); err != nil {
		return err
	}
	if err := e.save(); err != nil {
		return err
	}
	return printJSON(map[string]any{"milestone": ms, "issues": nums})
}

// ---- verify: 完了条件を wk が実行して判定する ----

type checkResult struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
}

type verifyResult struct {
	OK       bool          `json:"ok"`
	Issue    int           `json:"issue"`
	Head     string        `json:"head,omitempty"`
	Problems []string      `json:"problems,omitempty"`
	Checks   []checkResult `json:"checks"`
}

func verifiedPath(e *env) string { return filepath.Join(e.cfg.RuntimeDir, "verified.json") }

func loadVerified(e *env) map[string]string {
	m := map[string]string{}
	b, _ := os.ReadFile(verifiedPath(e))
	_ = json.Unmarshal(b, &m)
	return m
}

func verifiedKey(repo string, issue int) string { return fmt.Sprintf("%s#%d", repo, issue) }

// changedFiles は main から今の作業までに変わったファイル (コミットしたものと、まだのもの)。
func changedFiles(dir string) ([]string, bool, error) {
	base, err := git(dir, "merge-base", "origin/main", "HEAD")
	if err != nil {
		return nil, false, err
	}
	diff, err := git(dir, "diff", "--name-only", strings.TrimSpace(base), "HEAD")
	if err != nil {
		return nil, false, err
	}
	st, err := git(dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return nil, false, err
	}
	var files []string
	for _, f := range strings.Split(diff, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	dirty := false
	for _, l := range strings.Split(st, "\n") {
		if len(l) > 3 {
			dirty = true
			f := l[3:]
			if _, to, ok := strings.Cut(f, " -> "); ok {
				f = to
			}
			files = append(files, strings.Trim(f, `"`))
		}
	}
	slices.Sort(files)
	return slices.Compact(files), dirty, nil
}

func cmdVerify(args []string) error {
	e, err := load()
	if err != nil {
		return err
	}
	p, err := e.project()
	if err != nil {
		return err
	}
	num := 0
	if len(args) > 0 {
		if num, err = strconv.Atoi(args[0]); err != nil {
			return errors.New("使い方: wk verify [<issue>]")
		}
	} else if a, _, err := readCurrent(e); err == nil {
		num = a.Issue
	}
	tasks, err := state.LoadTasks(e.cfg.Root, p.Repo)
	if err != nil {
		return err
	}
	t := tasks.Get(num)
	if t == nil {
		return fmt.Errorf("#%d は %s の計画の task でない", num, p.Repo)
	}
	top, err := git("", "rev-parse", "--show-toplevel")
	if err != nil {
		return errors.New("アプリの worktree の中で実行する")
	}
	dir := strings.TrimSpace(top)
	r := verifyResult{Issue: num, Checks: []checkResult{}}
	_, _ = git(dir, "fetch", "-q", "origin", "main")
	// 受け入れテストは計画が書いたまま
	for f, want := range t.Tests {
		if got, err := fileSum(filepath.Join(dir, f)); err != nil || got != want {
			r.Problems = append(r.Problems, "受け入れテスト "+f+" が計画のときと違う (テストは変えない。git checkout origin/"+t.Branch+" -- "+f+" で戻す)")
		}
	}
	files, dirty, err := changedFiles(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if _, ok := t.Tests[f]; !ok && !t.Allowed(f) {
			r.Problems = append(r.Problems, "変えてよいファイルの外: "+f)
		}
	}
	if dirty {
		r.Problems = append(r.Problems, "コミットしていない変更がある (コミットしてからもう一度)")
	}
	if t.Setup != "" {
		if ok, out := runShell(dir, t.Setup, e.cfg.V4.CheckTimeout()); !ok {
			r.Problems = append(r.Problems, "setup ("+t.Setup+") が落ちる:\n"+out)
		}
	}
	all := true
	for _, c := range t.Checks {
		ok, out := runShell(dir, c.Run, e.cfg.V4.CheckTimeout())
		cr := checkResult{Name: c.Name, OK: ok}
		if !ok {
			cr.Output, all = out, false
		}
		r.Checks = append(r.Checks, cr)
	}
	r.OK = all && len(r.Problems) == 0
	if r.OK {
		head, err := git(dir, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		r.Head = strings.TrimSpace(head)
		m := loadVerified(e)
		m[verifiedKey(p.Repo, num)] = r.Head
		if err := os.WriteFile(verifiedPath(e), mustMarshal(m), 0o644); err != nil {
			return err
		}
	}
	if err := printJSON(r); err != nil || r.OK {
		return err
	}
	return exitError{1, fmt.Sprintf("#%d の完了条件をまだ満たしていない", num)}
}

func mustMarshal(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}

// taskMergeProblem は v4 の task の PR を merge してよいか (よければ "")。
// task のブランチ (wk/t<n>) の PR は、wk verify が通った今の先頭のコミットだけを merge する
func taskMergeProblem(e *env, repo string, pr int, head string) (string, error) {
	if !e.cfg.V4.Enabled {
		return "", nil
	}
	out, err := gh.GH("pr", "view", fmt.Sprint(pr), "--repo", repo, "--json", "headRefName", "--jq", ".headRefName")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(out)
	if a, _, err := readCurrent(e); err == nil && a.Phase == state.PhaseTask && branch != fmt.Sprintf("wk/t%d", a.Issue) {
		return fmt.Sprintf("task #%d の PR は wk/t%d のブランチから出す (今 %s)", a.Issue, a.Issue, branch), nil
	}
	var num int
	if _, err := fmt.Sscanf(branch, "wk/t%d", &num); err != nil {
		return "", nil
	}
	tasks, err := state.LoadTasks(e.cfg.Root, repo)
	if err != nil {
		return "", err
	}
	if tasks.Get(num) == nil {
		return "", nil
	}
	if loadVerified(e)[verifiedKey(repo, num)] != head {
		return fmt.Sprintf("今のコミットで wk verify が通っていない (task #%d。push する前に wk verify)", num), nil
	}
	return "", nil
}

// ---- retest: 計画が固定した受け入れテストを直す ----

// cmdRetest は task の受け入れテストを、計画の実行が直したものに替える (2026-11-24、#107 の AC-3 が CI の速さで落ちた)。
// 直したテストも今の main では落ちることを確かめ、テストのブランチに足して sha を書き直し、blocked と試行の数を外す。
// tick (task の段) の中では使えない: 作る側がテストをゆるめないように
func cmdRetest(args []string) error {
	num := 0
	if len(args) > 0 {
		num, _ = strconv.Atoi(args[0])
	}
	if num <= 0 {
		return errors.New("使い方: wk retest <issue> --reason <なぜ直すか> [--dir <worktree>]")
	}
	fs := flag.NewFlagSet("retest", flag.ContinueOnError)
	dirFlag := fs.String("dir", ".", "直したテストを置いたアプリの worktree")
	reason := fs.String("reason", "", "なぜ直すか (issue に書く)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*reason) == "" {
		return errors.New("--reason (なぜ直すか) が要る")
	}
	e, err := load()
	if err != nil {
		return err
	}
	if !e.cfg.V4.Enabled {
		return errors.New("v4 が有効でない (factory.json の v4.enabled)")
	}
	if a, _, err := readCurrent(e); err == nil && a.Phase == state.PhaseTask {
		return errors.New("task の tick の中では受け入れテストを直せない (直すのは計画。通らないなら wk attempt)")
	}
	p, err := e.project()
	if err != nil {
		return err
	}
	tasks, err := state.LoadTasks(e.cfg.Root, p.Repo)
	if err != nil {
		return err
	}
	t := tasks.Get(num)
	if t == nil {
		return fmt.Errorf("#%d は %s の計画の task でない", num, p.Repo)
	}
	dir, _ := filepath.Abs(*dirFlag)
	files := slices.Sorted(maps.Keys(t.Tests))
	body, sums, changed := map[string][]byte{}, map[string]string{}, false
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return fmt.Errorf("テスト %s: %w", f, err)
		}
		s := sha256.Sum256(b)
		body[f], sums[f] = b, hex.EncodeToString(s[:])
		changed = changed || sums[f] != t.Tests[f]
	}
	if !changed {
		return errors.New("受け入れテストが計画のときと同じ (直したファイルを --dir に置く)")
	}
	app := filepath.Join(e.cfg.AppsDir, filepath.Base(p.Repo))
	wt := filepath.Join(e.cfg.RuntimeDir, "plan", filepath.Base(p.Repo)+"-retest")
	if _, err := git(app, "fetch", "-q", "origin"); err != nil {
		return err
	}
	_, _ = git(app, "worktree", "remove", "--force", wt)
	_ = os.RemoveAll(wt)
	if _, err := git(app, "worktree", "add", "-q", "--detach", wt, "origin/main"); err != nil {
		return err
	}
	defer func() { _, _ = git(app, "worktree", "remove", "--force", wt) }()
	// 1. 直したテストも、今の main では落ちる (通るなら task の意味がない)
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(wt, f)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(wt, f), body[f], 0o644); err != nil {
			return err
		}
	}
	if t.Setup != "" {
		if ok, out := runShell(wt, t.Setup, e.cfg.V4.CheckTimeout()); !ok {
			return fmt.Errorf("setup (%s) が落ちる:\n%s", t.Setup, out)
		}
	}
	failing := 0
	for _, c := range t.Checks {
		if ok, _ := runShell(wt, c.Run, e.cfg.V4.CheckTimeout()); !ok {
			failing++
		}
	}
	if failing == 0 {
		return errors.New("直したテストでは、今の main で完了条件が全部通る (まだ通らないテストにする)")
	}
	// 2. テストのブランチに足す (作りかけのコミットは残す。force しない)。直したテストは main の上で 1 度コミットしてから持っていく
	steps := [][]string{
		append([]string{"add", "--"}, files...),
		{"commit", "-q", "--allow-empty", "-m", "retest"},
		{"checkout", "-q", "-B", "wk/retest"},
		{"checkout", "-q", "--detach", "origin/" + t.Branch},
		append([]string{"checkout", "wk/retest", "--"}, files...),
		append([]string{"add", "--"}, files...),
		{"commit", "-q", "--allow-empty", "-m", fmt.Sprintf("task: 受け入れテストを直す (#%d)", num)},
		{"push", "-q", "origin", "HEAD:refs/heads/" + t.Branch},
	}
	for _, st := range steps {
		if _, err := git(wt, st...); err != nil {
			return err
		}
	}
	t.Tests = sums
	if err := state.SaveTasks(e.cfg.Root, p.Repo, tasks); err != nil {
		return err
	}
	// 3. もう一度 tick が取れるように (試行の数は直す前のテストのもの)
	rm := []string{next.LabelBlocked}
	for i := 1; i <= max(e.cfg.V4.Attempts(), e.cfg.MaxAttemptsPerIssue); i++ {
		rm = append(rm, fmt.Sprintf("wk:attempt-%d", i))
	}
	if _, err := gh.GH("issue", "edit", fmt.Sprint(num), "--repo", p.Repo, "--remove-label", strings.Join(rm, ",")); err != nil {
		return err
	}
	if _, err := gh.GH("issue", "comment", fmt.Sprint(num), "--repo", p.Repo, "--body",
		"wakuwaku factory: 計画が受け入れテストを直した ("+strings.Join(files, ", ")+")。\n\n"+strings.TrimSpace(*reason)); err != nil {
		return err
	}
	return printJSON(map[string]any{"issue": num, "branch": t.Branch, "tests": sums})
}
