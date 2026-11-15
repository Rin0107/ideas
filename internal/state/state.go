// Package state は工場の状態を扱う。
//
//   - state/portfolio.json: アプリの一覧 (git で管理。wk だけが書く)
//   - state/ticks.jsonl:    tick の記録 (git で管理。wk だけが書く)
//   - state/meta.json:      intake・retro の最終日 (git で管理)
//   - <runtime_dir>/runtime.json: 今日の tick 数・連続失敗数 (worktree をまたいで共有。git の外)
package state

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// 状態
const (
	Selected   = "selected"
	Building   = "building"
	Evaluating = "evaluating"
	Iterating  = "iterating"
	Shipped    = "shipped"
	Dropped    = "dropped"
)

// 段階 (wk next が返す)
const (
	PhaseIdle     = "idle"
	PhaseRetro    = "retro"
	PhaseBuild    = "build"
	PhaseEvaluate = "evaluate"
	PhaseIterate  = "iterate"
	PhaseShip     = "ship"
	PhaseDrop     = "drop"
	PhaseIntake   = "intake"
	PhaseSelect   = "select"
	PhasePatrol   = "patrol" // ロールが自分の持ち場を見回り、仕事の提案を出す
	PhaseTriage   = "triage" // leader が提案を仕分ける (今やる・あとで・やらない)
	PhaseRepair   = "repair" // 工場そのものの直す issue (wk:fix) を解く
)

var validStatus = map[string]bool{Selected: true, Building: true, Evaluating: true, Iterating: true, Shipped: true, Dropped: true}

type Event struct {
	Date  string `json:"date"`
	Event string `json:"event"`
	Note  string `json:"note,omitempty"`
}

type Ticks struct {
	Total int `json:"total"`
	Phase int `json:"phase"` // 今の段階 (MVP / 今回のブラッシュアップ) で使った tick 数
}

type Project struct {
	Idea      string  `json:"idea"`
	Repo      string  `json:"repo"`
	Status    string  `json:"status"`
	Stack     string  `json:"stack"`
	Started   string  `json:"started"`
	Milestone string  `json:"milestone"`
	Iteration int     `json:"iteration"`
	Ticks     Ticks   `json:"ticks"`
	URL       string  `json:"url"`
	Preview   string  `json:"preview,omitempty"` // Web: いま評価に使っているプレビューの URL (wk deploy が書く)
	Verdict   string  `json:"verdict,omitempty"` // 直近の evaluate の結果 (ship / iterate / drop)。次の段階を決める
	History   []Event `json:"history"`
}

// Active は終わっていない (shipped / dropped でない) ものか。
func (p *Project) Active() bool { return p.Status != Shipped && p.Status != Dropped }

type Portfolio struct {
	Projects []*Project `json:"projects"`
}

// Active は作りかけのアプリを返す (なければ nil)。WIP=1 なので 1 つだけのはず。
func (pf *Portfolio) Active() *Project {
	for _, p := range pf.Projects {
		if p.Active() {
			return p
		}
	}
	return nil
}

// Tried は portfolio に載ったことのあるアイディアの集合。
func (pf *Portfolio) Tried() map[string]bool {
	m := map[string]bool{}
	for _, p := range pf.Projects {
		m[p.Idea] = true
	}
	return m
}

// Repos は工場が持っている repo の集合 (guard が使う)。
func (pf *Portfolio) Repos() map[string]bool {
	m := map[string]bool{}
	for _, p := range pf.Projects {
		if p.Repo != "" {
			m[p.Repo] = true
		}
	}
	return m
}

// Validate は portfolio の矛盾を返す。
func (pf *Portfolio) Validate() []string {
	var errs []string
	active, seenIdea, seenRepo := 0, map[string]bool{}, map[string]bool{}
	for i, p := range pf.Projects {
		at := fmt.Sprintf("projects[%d] (%s)", i, p.Idea)
		if p.Idea == "" {
			errs = append(errs, at+": idea が空")
		}
		if !validStatus[p.Status] {
			errs = append(errs, fmt.Sprintf("%s: status %q は不正", at, p.Status))
		}
		if p.Active() {
			active++
		}
		if seenIdea[p.Idea] {
			errs = append(errs, at+": 同じ idea が 2 回ある")
		}
		seenIdea[p.Idea] = true
		if p.Repo != "" {
			if seenRepo[p.Repo] {
				errs = append(errs, at+": 同じ repo が 2 回ある")
			}
			seenRepo[p.Repo] = true
		}
		if _, err := time.Parse(time.DateOnly, p.Started); err != nil {
			errs = append(errs, at+": started が YYYY-MM-DD でない")
		}
		if p.Status == Shipped && p.URL == "" {
			errs = append(errs, at+": shipped なのに url が空")
		}
	}
	if active > 1 {
		errs = append(errs, fmt.Sprintf("作りかけが %d 個ある (WIP は 1)", active))
	}
	return errs
}

// Meta は intake・retro・ロールごとの見回りの最終日。
type Meta struct {
	LastIntake string            `json:"last_intake"`
	LastRetro  string            `json:"last_retro"`
	LastPatrol map[string]string `json:"last_patrol,omitempty"` // ロール → 日付
}

// Runtime は worktree をまたいで共有する、その日の数え上げ。
type Runtime struct {
	Day                 string `json:"day"`
	TicksToday          int    `json:"ticks_today"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	// 1 回の実行 (run) で進めた段の数。run は worktree と時間で見分ける (Orca は毎回新しい worktree を作るが、
	// 同じ worktree が使い回されても、前の段からロックの有効期限より空いていれば別の実行とみなす)
	RunWorktree string    `json:"run_worktree,omitempty"`
	RunSteps    int       `json:"run_steps,omitempty"`
	RunSince    time.Time `json:"run_since,omitzero"` // この実行の最初の段
	RunLast     time.Time `json:"run_last,omitzero"`  // この実行の最後の段
}

// SameRun は、wd で now に始める段が、記録している実行の続きか (同じ worktree で、前の段から gap 以内)。
func (r *Runtime) SameRun(wd string, now time.Time, gap time.Duration) bool {
	return r.RunWorktree == wd && !now.After(r.RunLast.Add(gap))
}

// Step は wd で now に始める段を数える。続きでなければ新しい実行として 1 段目から数える。
func (r *Runtime) Step(wd string, now time.Time, gap time.Duration) {
	if !r.SameRun(wd, now, gap) {
		r.RunWorktree, r.RunSteps, r.RunSince = wd, 0, now
	}
	r.RunSteps++
	r.RunLast = now
}

// Tick は 1 tick の記録。
type Tick struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end,omitempty"`
	Phase  string    `json:"phase"`
	Repo   string    `json:"repo,omitempty"`
	Issue  int       `json:"issue,omitempty"`
	Role   string    `json:"role,omitempty"` // 担当したロール
	Result string    `json:"result"`         // ok / fail / aborted / skipped
	Note   string    `json:"note,omitempty"`
	// Tokens はこの段で使ったトークン (入力 + キャッシュ作成 + 出力。キャッシュの読み出しは CacheRead)。v3 の予算と「わくわく 1 点あたり」の材料
	Tokens    int `json:"tokens,omitempty"`
	CacheRead int `json:"cache_read,omitempty"`
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// mustJSON: 工場の型は必ず JSON にできる (失敗するならプログラムの誤りなので止める)。
func mustJSON(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

func writeJSON(path string, v any) error {
	b := mustJSON(json.MarshalIndent(v, "", "  "))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadPortfolio(root string) (*Portfolio, error) {
	pf := &Portfolio{}
	err := readJSON(filepath.Join(root, "state", "portfolio.json"), pf)
	return pf, err
}

func SavePortfolio(root string, pf *Portfolio) error {
	if pf.Projects == nil {
		pf.Projects = []*Project{}
	}
	return writeJSON(filepath.Join(root, "state", "portfolio.json"), pf)
}

func LoadMeta(root string) (*Meta, error) {
	m := &Meta{}
	return m, readJSON(filepath.Join(root, "state", "meta.json"), m)
}

func SaveMeta(root string, m *Meta) error {
	return writeJSON(filepath.Join(root, "state", "meta.json"), m)
}

// LoadRuntime は今日の分を返す。日付が変わっていれば tick 数と実行の段の数を 0 に戻す (連続失敗数は持ち越す)。
func LoadRuntime(dir string, now time.Time) (*Runtime, error) {
	r := &Runtime{}
	if err := readJSON(filepath.Join(dir, "runtime.json"), r); err != nil {
		return nil, err
	}
	if today := now.Format(time.DateOnly); r.Day != today {
		r.Day, r.TicksToday = today, 0
		r.RunWorktree, r.RunSteps, r.RunSince, r.RunLast = "", 0, time.Time{}, time.Time{}
	}
	return r, nil
}

func SaveRuntime(dir string, r *Runtime) error {
	return writeJSON(filepath.Join(dir, "runtime.json"), r)
}

func AppendTick(root string, t Tick) error {
	p := filepath.Join(root, "state", "ticks.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(mustJSON(json.Marshal(t)), '\n'))
	return err
}

func ReadTicks(root string) ([]Tick, error) {
	f, err := os.Open(filepath.Join(root, "state", "ticks.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var ts []Tick
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var t Tick
		if err := json.Unmarshal(sc.Bytes(), &t); err != nil {
			return nil, fmt.Errorf("ticks.jsonl:%d: %w", n, err)
		}
		ts = append(ts, t)
	}
	return ts, sc.Err()
}

// DaysSince は日付文字列 (空なら十分昔) から now までの日数。
func DaysSince(date string, now time.Time) int {
	if date == "" {
		return 1 << 20
	}
	t, err := time.ParseInLocation(time.DateOnly, date, now.Location())
	if err != nil {
		return 1 << 20
	}
	return int(now.Sub(t).Hours() / 24)
}
