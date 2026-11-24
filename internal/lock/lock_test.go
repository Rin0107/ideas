package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	ttl := time.Hour
	if stale, err := Acquire(dir, "k", Info{Since: now, Phase: "build"}, ttl); err != nil || stale != nil {
		t.Fatal(stale, err)
	}
	if _, err := Acquire(dir, "k", Info{Since: now.Add(time.Minute), Phase: "select"}, ttl); err == nil {
		t.Fatal("生きているロックがあるのに取れた")
	}
	if err := Release(dir, "k"); err != nil {
		t.Fatal(err)
	}
	if err := Release(dir, "k"); err != nil {
		t.Fatal("2 回目の Release でエラー")
	}
	if _, err := Acquire(dir, "k", Info{Since: now, Phase: "x"}, ttl); err != nil {
		t.Fatal(err)
	}
}

func TestStaleLockIsReplaced(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-3 * time.Hour)
	if _, err := Acquire(dir, "k", Info{Since: old, Phase: "build", Worktree: "/w/1"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	stale, err := Acquire(dir, "k", Info{Since: time.Now(), Phase: "evaluate"}, time.Hour)
	if err != nil || stale == nil || stale.Worktree != "/w/1" {
		t.Fatal(stale, err)
	}
	cur, _ := Read(dir, "k")
	if cur.Phase != "evaluate" {
		t.Fatal(cur)
	}
}

func TestBrokenLockIsStale(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tick-k.lock"), []byte("{壊れた"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir, "k", Info{Since: time.Now()}, time.Hour); err != nil {
		t.Fatal("壊れたロックを古いものとして扱えない:", err)
	}
}

func TestListAndClaim(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if err := Claim(dir, "a", "R/x", "1-x"); err == nil {
		t.Fatal("ロックがないのに書けた")
	}
	if _, err := Acquire(dir, "a", Info{Since: now, Phase: "build", Repo: "R/a"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir, "b", Info{Since: now, Phase: "select"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := Claim(dir, "b", "", "2-b"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ls, err := List(dir)
	if err != nil || len(ls) != 2 || ls["a"].Repo != "R/a" || ls["b"].Idea != "2-b" || ls["b"].Phase != "select" {
		t.Fatal(ls, err)
	}
	if err := os.Remove(Path(dir, "a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(Path(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := List(dir); err == nil {
		t.Fatal("読めないロックがあるのに一覧できた")
	}
	if err := Claim(dir, "a", "", ""); err == nil {
		t.Fatal("読めないロックに書けた")
	}
}

func TestHold(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s", "state.lock")
	rel, err := Hold(p, time.Second, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := Hold(p, 30*time.Millisecond, 5*time.Millisecond); err == nil {
		t.Fatal("持たれているのに取れた")
	}
	if time.Since(start) < 30*time.Millisecond {
		t.Fatal("待たずに諦めた")
	}
	go func() { time.Sleep(20 * time.Millisecond); rel() }()
	rel2, err := Hold(p, time.Second, time.Millisecond)
	if err != nil {
		t.Fatal("外したあとに取れない:", err)
	}
	rel2()
	// 置き場所を作れない・開けない
	f := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Hold(filepath.Join(f, "x", "state.lock"), time.Second, time.Millisecond); err == nil {
		t.Fatal("置き場所を作れないのに取れた")
	}
	if _, err := Hold(t.TempDir(), time.Second, time.Millisecond); err == nil {
		t.Fatal("ディレクトリを開けた")
	}
}

// fakeProc は /proc の代わり。procs は pid → "名前 親 開始時刻"
func fakeProc(t *testing.T, procs map[int][3]string) {
	t.Helper()
	root := t.TempDir()
	for pid, p := range procs {
		dir := filepath.Join(root, fmt.Sprint(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// 実物と同じ並び: pid (comm) state ppid … 22 番目が starttime。名前には空白と ) も入りうる
		f := make([]string, 20)
		for i := range f {
			f[i] = "0"
		}
		f[0], f[1], f[19] = "S", p[1], p[2]
		line := fmt.Sprintf("%d (%s) %s\n", pid, p[0], strings.Join(f, " "))
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := ProcRoot
	ProcRoot = root
	t.Cleanup(func() { ProcRoot = old })
}

func TestOwnerAndAlive(t *testing.T) {
	fakeProc(t, map[int][3]string{
		10: {"bash", "9", "300"},
		9:  {"claude", "2", "200"},
		2:  {"init", "1", "100"},
		20: {"a (b) c", "9", "400"},
		30: {"x", "31", "1"},
		31: {"y", "30", "1"},
	})
	if pid, st := Owner(10, "claude"); pid != 9 || st != "200" {
		t.Fatal(pid, st)
	}
	if pid, st := Owner(9, "claude"); pid != 9 || st != "200" {
		t.Fatal("自分自身が持ち主", pid, st)
	}
	if pid, _ := Owner(20, "claude"); pid != 9 {
		t.Fatal("名前に ) があっても親を読む", pid)
	}
	if pid, st := Owner(10, "codex"); pid != 0 || st != "" {
		t.Fatal("見つからなければ 0", pid, st)
	}
	if pid, _ := Owner(30, "claude"); pid != 0 {
		t.Fatal("親が輪になっても止まる", pid)
	}
	if pid, _ := Owner(77, "claude"); pid != 0 {
		t.Fatal("読めない pid", pid)
	}
	if !Alive(9, "200") || !Alive(9, "") || Alive(9, "201") || Alive(77, "") {
		t.Fatal("Alive")
	}
}

func TestOwnerStopsAtInit(t *testing.T) {
	// pid 1 は見ない (init が claude という名前でも持ち主にしない)
	fakeProc(t, map[int][3]string{5: {"bash", "1", "1"}, 1: {"claude", "0", "1"}})
	if pid, _ := Owner(5, "claude"); pid != 0 {
		t.Fatal(pid)
	}
	if pid, _ := Owner(1, "claude"); pid != 0 {
		t.Fatal(pid)
	}
}

func TestStatBroken(t *testing.T) {
	root := t.TempDir()
	for pid, line := range map[string]string{"3": "3 no-paren S 1", "4": "4 (short) S 1 2", "5": ") (x"} {
		if err := os.MkdirAll(filepath.Join(root, pid), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, pid, "stat"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := ProcRoot
	ProcRoot = root
	t.Cleanup(func() { ProcRoot = old })
	for _, pid := range []int{3, 4, 5} {
		if Alive(pid, "") {
			t.Fatal("壊れた stat を生きているとした", pid)
		}
	}
}

func TestStaleWhenOwnerGone(t *testing.T) {
	fakeProc(t, map[int][3]string{9: {"claude", "2", "200"}})
	now := time.Now()
	for _, c := range []struct {
		i     Info
		stale bool
	}{
		{Info{Since: now, Pid: 9, Started: "200"}, false},
		{Info{Since: now, Pid: 9, Started: "999"}, true}, // 番号が使い回された
		{Info{Since: now, Pid: 8}, true},                 // 持ち主が終わった
		{Info{Since: now}, false},                        // 持ち主が分からない: TTL だけ
		{Info{Since: now.Add(-2 * time.Hour), Pid: 9, Started: "200"}, true},
	} {
		if got := c.i.Stale(now, time.Hour); got != c.stale {
			t.Fatalf("%+v: %v", c.i, got)
		}
	}
	dir := t.TempDir()
	if _, err := Acquire(dir, "k", Info{Since: now, Pid: 8, Worktree: "/w/dead"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	stale, err := Acquire(dir, "k", Info{Since: now, Pid: 9, Started: "200"}, time.Hour)
	if err != nil || stale == nil || stale.Worktree != "/w/dead" {
		t.Fatal("落ちた持ち主のロックを引き継げない", stale, err)
	}
}

func TestOwnerReal(t *testing.T) {
	// 本物の /proc: このテストのプロセス自身を名前で見つける
	_, _, _, ok := stat(os.Getpid())
	if !ok {
		t.Skip("/proc がない")
	}
	name, _, started, _ := stat(os.Getpid())
	if pid, st := Owner(os.Getpid(), name); pid != os.Getpid() || st != started || !Alive(pid, st) {
		t.Fatal(pid, st)
	}
}
