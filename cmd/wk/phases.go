package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Rin0107/ideas/internal/gh"
	"github.com/Rin0107/ideas/internal/ideas"
	"github.com/Rin0107/ideas/internal/next"
	"github.com/Rin0107/ideas/internal/role"
	"github.com/Rin0107/ideas/internal/state"
	"github.com/Rin0107/ideas/internal/verdict"
)

// Plan は select / iterate で LLM が書く計画。wk が形を検査してから GitHub に登録する。
type Plan struct {
	RepoName    string      `json:"repo_name,omitempty"` // scaffold のときだけ
	Description string      `json:"description,omitempty"`
	Topics      []string    `json:"topics,omitempty"`
	Goal        string      `json:"goal"` // このマイルストーンで成立させる体験 (1〜2 文)
	Issues      []PlanIssue `json:"issues"`
}

type PlanIssue struct {
	Title string   `json:"title"`
	Body  string   `json:"body"`
	AC    []string `json:"ac"`             // 受け入れ条件。テストにできる文
	Role  string   `json:"role,omitempty"` // 担当のロール (engineer / designer / sre)。空なら engineer
}

var repoNameRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

func (p Plan) validate(scaffold bool, maxIssues int) error {
	var errs []string
	if scaffold {
		if !repoNameRe.MatchString(p.RepoName) || len(p.RepoName) > 40 {
			errs = append(errs, "repo_name は英小文字とハイフン、40 文字まで")
		}
		if strings.TrimSpace(p.Description) == "" || len([]rune(p.Description)) > 120 {
			errs = append(errs, "description は 1〜120 文字")
		}
		if len(p.Topics) == 0 {
			errs = append(errs, "topics が空")
		}
	}
	if strings.TrimSpace(p.Goal) == "" {
		errs = append(errs, "goal が空")
	}
	min := 3
	if !scaffold {
		min = 1
	}
	if len(p.Issues) < min || len(p.Issues) > maxIssues {
		errs = append(errs, fmt.Sprintf("issue は %d〜%d 件 (今 %d 件)", min, maxIssues, len(p.Issues)))
	}
	errs = append(errs, issueErrors(p.Issues)...)
	if len(errs) > 0 {
		return errors.New("計画の形が不正:\n  " + strings.Join(errs, "\n  "))
	}
	return nil
}

// issueErrors は issue の形の誤りを返す (計画でも、ロールの提案でも同じ決まり)。
func issueErrors(issues []PlanIssue) []string {
	var errs []string
	for i, is := range issues {
		if strings.TrimSpace(is.Title) == "" {
			errs = append(errs, fmt.Sprintf("issues[%d]: title が空", i))
		}
		if len(is.AC) < 1 || len(is.AC) > 5 {
			errs = append(errs, fmt.Sprintf("issues[%d]: ac は 1〜5 件", i))
		}
		for k, ac := range is.AC {
			if len([]rune(strings.TrimSpace(ac))) < 8 {
				errs = append(errs, fmt.Sprintf("issues[%d].ac[%d]: 短すぎる (テストにできる文にする)", i, k))
			}
		}
		if is.Role != "" && !role.Valid(is.Role) {
			errs = append(errs, fmt.Sprintf("issues[%d]: role %q は不正 (engineer / designer / sre)", i, is.Role))
		}
	}
	return errs
}

func issueBody(is PlanIssue) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(is.Body))
	b.WriteString("\n\n## 受け入れ条件\nテスト名に `AC-<この issue の番号>-<n>` を入れる (例: AC-3-1)。\n\n")
	for k, ac := range is.AC {
		fmt.Fprintf(&b, "- [ ] AC-%d: %s\n", k+1, strings.TrimSpace(ac))
	}
	return b.String()
}

// 引数は各コマンドで flag.NewFlagSet で読む (関数を渡す形にすると、その後ろの err != nil がカバレッジの区切りから外れ、
// ミューテーションが「テストが通らない」と数える。2026-11-10)

func (e *env) project() (*state.Project, error) {
	p := e.pf.Active()
	if p == nil {
		return nil, errors.New("作っているアプリがない")
	}
	return p, nil
}

func (e *env) save() error { return state.SavePortfolio(e.cfg.Root, e.pf) }

func (e *env) event(p *state.Project, ev, note string) {
	p.History = append(p.History, state.Event{Date: e.today(), Event: ev, Note: note})
}

// appendIdeaHistory は idea.md の「## 履歴」に 1 行足す。
func (e *env) appendIdeaHistory(idea, line string) error {
	path := filepath.Join(e.cfg.Root, e.cfg.IdeasDir, idea, "idea.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := strings.TrimRight(string(b), "\n") + "\n"
	if !strings.Contains(s, "\n## 履歴\n") {
		s += "\n## 履歴\n"
	}
	s += "- " + e.today() + " " + line + "\n"
	return os.WriteFile(path, []byte(s), 0o644)
}

// ---- select ----

func cmdSelect(args []string) error {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	scoresPath := fs.String("scores", "", "点数の JSON (- で標準入力)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	if p := e.pf.Active(); p != nil {
		return fmt.Errorf("作りかけがある: %s (%s)", p.Idea, p.Status)
	}
	var scores []verdict.SelectScore
	if err := verdict.ReadJSON(*scoresPath, &scores); err != nil {
		return err
	}
	all, err := ideas.Load(filepath.Join(e.cfg.Root, e.cfg.IdeasDir))
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, c := range ideas.Candidates(all, e.pf.Tried(), e.cfg.Select.Candidates, e.today()) {
		allowed[c.Dir] = true
	}
	var skipped []string
	if e.cfg.Site.URL == "" { // 配信の準備がまだなら Web は作らない (出せないため)
		scores = slices.DeleteFunc(scores, func(s verdict.SelectScore) bool {
			if s.Stack == "web" {
				skipped = append(skipped, s.Idea+": 配信の準備がまだ (factory.json の site.url) なので Web は除外")
				return true
			}
			return false
		})
	}
	best, log, err := verdict.ChooseIdea(scores, allowed, e.cfg.Select)
	for _, l := range append(skipped, log...) {
		fmt.Fprintln(os.Stderr, l)
	}
	if err != nil {
		return err
	}
	note := fmt.Sprintf("合計 %d/25 (わくわく%d・軽さ%d・無料%d・自分ごと%d・Findy%d) %s", best.Total(), best.Wakuwaku, best.Light,
		best.Free, best.Own, best.Findy, best.Reason)
	p := &state.Project{Idea: best.Idea, Status: state.Selected, Stack: best.Stack, Started: e.today(), Milestone: "MVP"}
	e.event(p, "selected", note)
	e.pf.Projects = append(e.pf.Projects, p)
	if err := e.appendIdeaHistory(best.Idea, "選定: "+note); err != nil {
		return err
	}
	if err := e.save(); err != nil {
		return err
	}
	return printJSON(map[string]any{"idea": best.Idea, "stack": best.Stack, "total": best.Total()})
}

// ---- scaffold ----

func cmdScaffold(args []string) error {
	fs := flag.NewFlagSet("scaffold", flag.ContinueOnError)
	planPath := fs.String("plan", "", "計画の JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	p, err := e.project()
	if err != nil {
		return err
	}
	if p.Status != state.Selected {
		return fmt.Errorf("%s は selected でない (%s)", p.Idea, p.Status)
	}
	var plan Plan
	if err := verdict.ReadJSON(*planPath, &plan); err != nil {
		return err
	}
	if err := plan.validate(true, e.cfg.MaxOpenIssues); err != nil {
		return err
	}
	repo := e.cfg.Owner + "/" + plan.RepoName
	dir := filepath.Join(e.cfg.AppsDir, plan.RepoName)

	exists := false
	if _, err := gh.GH("repo", "view", repo, "--json", "name"); err == nil {
		exists = true
		if !e.pf.Repos()[repo] && p.Repo != repo {
			return fmt.Errorf("%s はすでにある (工場のものではない)。repo_name を変える", repo)
		}
	}
	p.Repo = repo // guard がこの repo への書き込みを許すように、先に記録する
	if err := e.save(); err != nil {
		return err
	}
	if !exists {
		if err := copyTemplate(filepath.Join(e.cfg.Root, "templates", p.Stack), dir, map[string]string{
			"{{NAME}}": plan.RepoName, "{{DESCRIPTION}}": plan.Description, "{{REPO}}": repo, "{{GOAL}}": plan.Goal,
			"{{IDEA}}": p.Idea, "{{URL}}": "(まだ公開していません)",
		}); err != nil {
			return err
		}
		for _, c := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "雛形から作成 (wakuwaku factory)"}} {
			if _, err := gh.Run(dir, "git", c...); err != nil {
				return err
			}
		}
		if err := gh.CreateAndPush(repo, dir, plan.Description); err != nil {
			return err
		}
	}
	if len(plan.Topics) > 0 {
		args := []string{"repo", "edit", repo}
		for _, t := range plan.Topics {
			args = append(args, "--add-topic", t)
		}
		if _, err := gh.GH(append(args, "--add-topic", "wakuwaku-factory")...); err != nil {
			return err
		}
	}
	if err := ensureLabels(repo, e.cfg.MaxAttemptsPerIssue); err != nil {
		return err
	}
	if err := createMilestone(repo, "MVP", plan.Goal); err != nil {
		return err
	}
	open, err := gh.OpenIssues(repo, "MVP")
	if err != nil {
		return err
	}
	if len(open) == 0 { // 途中で落ちて再開したときに二重登録しない
		if err := createIssues(repo, "MVP", plan.Issues); err != nil {
			return err
		}
	}
	p.Status = state.Building
	p.Ticks.Phase = 0
	e.event(p, "scaffolded", repo+" / issue "+fmt.Sprint(len(plan.Issues))+" 件")
	if err := e.appendIdeaHistory(p.Idea, "着手: "+repo+" — MVP: "+plan.Goal); err != nil {
		return err
	}
	if err := e.save(); err != nil {
		return err
	}
	fmt.Println("作成:", repo, dir)
	return nil
}

func copyTemplate(src, dst string, repl map[string]string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s はすでにある", dst)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if d.Name() == "node_modules" || d.Name() == "dist" {
			return filepath.SkipDir
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s := string(b)
		for k, v := range repl {
			s = strings.ReplaceAll(s, k, v)
		}
		info, _ := d.Info()
		return os.WriteFile(out, []byte(s), info.Mode().Perm())
	})
}

func ensureLabels(repo string, maxAttempts int) error {
	labels := map[string]string{
		"wk:task": "0e8a16", "wk:blocked": "b60205", "wk:reviewed": "1d76db", "wk:incident": "d93f0b", next.LabelFix: "0e8a16", labelHuman: "d93f0b",
		labelProposal: "c5def5", labelLater: "d4c5f9",
	}
	for _, r := range role.Workers() {
		labels[role.Label(r)] = "bfdadc"
	}
	for i := 1; i <= maxAttempts; i++ {
		labels[fmt.Sprintf("wk:attempt-%d", i)] = "fbca04"
	}
	for name, color := range labels {
		if _, err := gh.GH("label", "create", name, "--repo", repo, "--color", color, "--force"); err != nil {
			return err
		}
	}
	return nil
}

func createMilestone(repo, title, desc string) error {
	out, err := gh.GH("api", "repos/"+repo+"/milestones", "--jq", ".[].title")
	if err != nil {
		return err
	}
	for _, t := range strings.Split(out, "\n") {
		if t == title {
			return nil
		}
	}
	_, err = gh.GH("api", "-X", "POST", "repos/"+repo+"/milestones", "-f", "title="+title, "-f", "description="+desc)
	return err
}

func createIssues(repo, milestone string, issues []PlanIssue) error {
	for _, is := range issues {
		if _, err := gh.GH("issue", "create", "--repo", repo, "--title", is.Title, "--body", issueBody(is),
			"--milestone", milestone, "--label", "wk:task", "--label", role.Label(roleOr(is.Role, role.Engineer))); err != nil {
			return err
		}
	}
	return nil
}

// ---- attempt ----

func cmdAttempt(args []string) error {
	if len(args) < 1 {
		return errors.New("使い方: wk attempt <issue> --note ...")
	}
	var num int
	if _, err := fmt.Sscan(args[0], &num); err != nil {
		return err
	}
	fs := flag.NewFlagSet("attempt", flag.ContinueOnError)
	note := fs.String("note", "", "失敗の理由")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	p, err := e.project()
	if err != nil {
		return err
	}
	open, err := gh.OpenIssues(p.Repo, p.Milestone)
	if err != nil {
		return err
	}
	var is *next.Issue
	for i := range open {
		if open[i].Number == num {
			is = &open[i]
		}
	}
	if is == nil {
		return fmt.Errorf("#%d は %s の open な issue にない", num, p.Milestone)
	}
	n := is.Attempts() + 1
	add := fmt.Sprintf("wk:attempt-%d", n)
	if n >= e.cfg.MaxAttemptsPerIssue {
		add += "," + next.LabelBlocked
	}
	if _, err := gh.GH("issue", "edit", fmt.Sprint(num), "--repo", p.Repo, "--add-label", add); err != nil {
		return err
	}
	_, err = gh.GH("issue", "comment", fmt.Sprint(num), "--repo", p.Repo, "--body",
		fmt.Sprintf("wakuwaku factory: 試行 %d/%d が失敗。\n\n%s", n, e.cfg.MaxAttemptsPerIssue, *note))
	fmt.Println("記録:", add)
	return err
}

// ---- evaluate ----

func cmdEvaluate(args []string) error {
	fs := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	scoresPath := fs.String("scores", "", "評価の JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	p, err := e.project()
	if err != nil {
		return err
	}
	var sc verdict.EvalScore
	if err := verdict.ReadJSON(*scoresPath, &sc); err != nil {
		return err
	}
	v, why, err := verdict.Decide(sc, p.Iteration, e.cfg.MaxIterations, e.cfg.Evaluate)
	if err != nil {
		return err
	}
	p.Status, p.Verdict = state.Evaluating, v
	n := len(filterEvents(p.History, "evaluated")) + 1
	e.event(p, "evaluated", v+": "+why)
	report := fmt.Sprintf("# 評価 %d: %s (%s)\n\n- 日付: %s\n- 結論: **%s** — %s\n- 核が動く: %v / わくわく %d / 品質 %d\n- 一番の弱点: %s\n\n## まとめ\n%s\n\n## バグ\n",
		n, p.Repo, p.Milestone, e.today(), v, why, sc.CoreWorks, sc.Wakuwaku, sc.Quality, sc.Weakest, sc.Summary)
	for _, b := range sc.Bugs {
		report += "- " + b + "\n"
	}
	name := strings.ReplaceAll(p.Repo, "/", "_") + fmt.Sprintf("-%d.md", n)
	if err := os.MkdirAll(filepath.Join(e.cfg.Root, "reports", "evaluations"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(e.cfg.Root, "reports", "evaluations", name), []byte(report), 0o644); err != nil {
		return err
	}
	if err := e.appendIdeaHistory(p.Idea, fmt.Sprintf("評価%d: %s — %s。弱点: %s", n, v, why, sc.Weakest)); err != nil {
		return err
	}
	if err := e.save(); err != nil {
		return err
	}
	return printJSON(map[string]string{"verdict": v, "reason": why, "report": "reports/evaluations/" + name})
}

func filterEvents(h []state.Event, ev string) []state.Event {
	var out []state.Event
	for _, x := range h {
		if x.Event == ev {
			out = append(out, x)
		}
	}
	return out
}

// ---- iterate ----

func cmdIterate(args []string) error {
	fs := flag.NewFlagSet("iterate", flag.ContinueOnError)
	planPath := fs.String("plan", "", "計画の JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	p, err := e.project()
	if err != nil {
		return err
	}
	if p.Verdict != "iterate" {
		return fmt.Errorf("評価が iterate でない (%q)", p.Verdict)
	}
	var plan Plan
	if err := verdict.ReadJSON(*planPath, &plan); err != nil {
		return err
	}
	if err := plan.validate(false, 5); err != nil {
		return err
	}
	ms := fmt.Sprintf("v0.%d", p.Iteration+2)
	if err := createMilestone(p.Repo, ms, plan.Goal); err != nil {
		return err
	}
	if open, err := gh.OpenIssues(p.Repo, ms); err != nil {
		return err
	} else if len(open) == 0 {
		if err := createIssues(p.Repo, ms, plan.Issues); err != nil {
			return err
		}
	}
	p.Iteration++
	p.Milestone, p.Status, p.Verdict, p.Ticks.Phase = ms, state.Iterating, "", 0
	e.event(p, "iterating", ms+": "+plan.Goal)
	if err := e.appendIdeaHistory(p.Idea, "ブラッシュアップ "+ms+": "+plan.Goal); err != nil {
		return err
	}
	return e.save()
}

// ---- ship ----

func cmdShip(args []string) error {
	fs := flag.NewFlagSet("ship", flag.ContinueOnError)
	url := fs.String("url", "", "本番 URL (CLI なら release の URL)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	p, err := e.project()
	if err != nil {
		return err
	}
	if p.Verdict != "ship" {
		return fmt.Errorf("評価が ship でない (%q)", p.Verdict)
	}
	if p.Stack == "web" {
		if e.cfg.Site.URL == "" {
			return errors.New("配信の準備がまだ (factory.json の site.url。docs/SETUP.md)")
		}
		*url = strings.TrimRight(e.cfg.Site.URL, "/") + "/" + filepath.Base(p.Repo) + "/"
	}
	if probs := publishCheck(p.Repo, *url, p.Stack == "web"); len(probs) > 0 {
		return errors.New("公開の点検に通らない:\n  " + strings.Join(probs, "\n  "))
	}
	if p.Stack == "web" {
		got, err := publishWeb(e, p)
		if err != nil {
			return err
		}
		*url = got
	}
	if _, err := gh.GH("repo", "edit", p.Repo, "--visibility", "public", "--accept-visibility-change-consequences"); err != nil {
		return err
	}
	p.Status, p.URL, p.Preview, p.Verdict = state.Shipped, *url, "", ""
	e.event(p, "shipped", *url)
	if err := e.appendIdeaHistory(p.Idea, "公開: "+*url+" ("+p.Repo+")"); err != nil {
		return err
	}
	return e.save()
}

// publishCheck は public にする前の点検。web のときは本番に出す前なので、URL は README に載っているかだけを見る
// (本番の検査は publishWeb がする)。
func publishCheck(repo, url string, web bool) []string {
	var probs []string
	if readme, err := gh.GH("api", "repos/"+repo+"/readme", "-H", "Accept: application/vnd.github.raw"); err == nil && url != "" && !strings.Contains(readme, url) {
		probs = append(probs, "README に公開の URL ("+url+") がない")
	}
	for _, f := range []string{"README.md", "LICENSE"} {
		if _, err := gh.GH("api", "repos/"+repo+"/contents/"+f, "--jq", ".name"); err != nil {
			probs = append(probs, f+" がない")
		}
	}
	if out, err := gh.GH("repo", "view", repo, "--json", "description,repositoryTopics", "--jq",
		`(.description|length|tostring) + " " + (.repositoryTopics|length|tostring)`); err != nil {
		probs = append(probs, err.Error())
	} else if f := strings.Fields(out); len(f) != 2 || f[0] == "0" || f[1] == "0" {
		probs = append(probs, "description か topics が空")
	}
	if out, err := gh.GH("run", "list", "--repo", repo, "--branch", "main", "--limit", "1", "--json", "conclusion", "--jq", ".[0].conclusion"); err != nil || strings.TrimSpace(out) != "success" {
		probs = append(probs, "main の最新の CI が success でない ("+strings.TrimSpace(out)+")")
	}
	if url == "" {
		probs = append(probs, "--url が空")
	} else if web {
		// 本番に出すのはこのあと
	} else if code := httpStatus(url); code != 200 {
		probs = append(probs, fmt.Sprintf("%s が %d", url, code))
	}
	return probs
}

func httpStatus(url string) int {
	c := http.Client{Timeout: 20 * time.Second, Transport: httpTransport}
	r, err := c.Get(url)
	if err != nil {
		return 0
	}
	r.Body.Close()
	return r.StatusCode
}

// ---- drop ----

func cmdDrop(args []string) error {
	fs := flag.NewFlagSet("drop", flag.ContinueOnError)
	reason := fs.String("reason", "", "捨てる理由 (次の選定に効く形で)")
	never := fs.Bool("never", false, "二度と選ばない")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len([]rune(strings.TrimSpace(*reason))) < 10 {
		return errors.New("--reason を具体的に書く (10 文字以上)")
	}
	e, err := load()
	if err != nil {
		return err
	}
	p, err := e.project()
	if err != nil {
		return err
	}
	if p.Stack == "web" {
		if err := unpublishWeb(e, p); err != nil {
			return fmt.Errorf("本番から消せない: %w", err)
		}
	}
	if p.Repo != "" {
		if _, err := gh.GH("repo", "archive", p.Repo, "--yes"); err != nil {
			return err
		}
	}
	p.Status, p.Verdict = state.Dropped, ""
	e.event(p, "dropped", *reason)
	line := "破棄: " + *reason
	if *never {
		line += " (wk:never)"
	}
	if err := e.appendIdeaHistory(p.Idea, line); err != nil {
		return err
	}
	return e.save()
}
