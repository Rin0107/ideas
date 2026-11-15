package lock

import (
	"os"
	"path/filepath"
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
