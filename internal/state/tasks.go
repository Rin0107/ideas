package state

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// v4 (docs/DESIGN-v4.md): 計画が作った task の完了条件。wk だけが書く (state/tasks/<repo の名前>.json)。
// issue の本文は人が読む写しで、wk verify はここだけを信じる (issue は誰でも書き換えられる)

// Check は完了条件 1 つ: Run を app の worktree で実行し、終了コード 0 なら満たした。
type Check struct {
	Name string `json:"name"`
	Run  string `json:"run"`
}

type Task struct {
	Issue  int    `json:"issue"`
	Title  string `json:"title"`
	Branch string `json:"branch"` // 計画がテストを入れたブランチ (wk/t<issue>)。tick はこの上で作る
	// Tests は計画が書いた受け入れテストのファイルと、その sha256。tick は変えてはいけない
	Tests map[string]string `json:"tests"`
	// Paths は tick が変えてよいファイル (filepath.Match の形。末尾の /** はその下全部)
	Paths  []string `json:"paths"`
	Setup  string   `json:"setup,omitempty"` // 完了条件の前に 1 回 (npm ci など)
	Checks []Check  `json:"checks"`
	After  []int    `json:"after,omitempty"` // 先に終わっていてほしい task の issue
	From   []string `json:"from,omitempty"`  // 元になった人の issue (owner/repo#n)
	Plan   string   `json:"plan"`            // 計画した日
}

type Tasks struct {
	Tasks map[string]*Task `json:"tasks"` // key は issue の番号
}

func tasksPath(root, repo string) string {
	return filepath.Join(root, "state", "tasks", filepath.Base(repo)+".json")
}

// LoadTasks は repo の task (なければ空)。
func LoadTasks(root, repo string) (*Tasks, error) {
	t := &Tasks{}
	err := readJSON(tasksPath(root, repo), t)
	if t.Tasks == nil {
		t.Tasks = map[string]*Task{}
	}
	return t, err
}

func SaveTasks(root, repo string, t *Tasks) error { return writeJSON(tasksPath(root, repo), t) }

// Get は issue の task (なければ nil)。
func (t *Tasks) Get(issue int) *Task { return t.Tasks[strconv.Itoa(issue)] }

func (t *Tasks) Put(task *Task) { t.Tasks[strconv.Itoa(task.Issue)] = task }

// Allowed は tick がそのファイルを変えてよいか (Paths のどれかに当たり、テストのファイルでない)。
func (t *Task) Allowed(file string) bool {
	if _, ok := t.Tests[file]; ok {
		return false
	}
	for _, p := range t.Paths {
		if dir, ok := strings.CutSuffix(p, "/**"); ok {
			if strings.HasPrefix(file, dir+"/") {
				return true
			}
			continue
		}
		if m, _ := filepath.Match(p, file); m {
			return true
		}
	}
	return false
}

// PlanNight は t が属する夜の日付 (正午から次の正午まで)。23:10 に始めた計画が 0 時を過ぎて終わっても、同じ夜に数える
func PlanNight(t time.Time) string { return t.Add(-12 * time.Hour).Format(time.DateOnly) }
