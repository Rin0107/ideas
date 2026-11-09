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
	if stale, err := Acquire(dir, Info{Since: now, Phase: "build"}, ttl); err != nil || stale != nil {
		t.Fatal(stale, err)
	}
	if _, err := Acquire(dir, Info{Since: now.Add(time.Minute), Phase: "select"}, ttl); err == nil {
		t.Fatal("生きているロックがあるのに取れた")
	}
	if err := Release(dir); err != nil {
		t.Fatal(err)
	}
	if err := Release(dir); err != nil {
		t.Fatal("2 回目の Release でエラー")
	}
	if _, err := Acquire(dir, Info{Since: now, Phase: "x"}, ttl); err != nil {
		t.Fatal(err)
	}
}

func TestStaleLockIsReplaced(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-3 * time.Hour)
	if _, err := Acquire(dir, Info{Since: old, Phase: "build", Worktree: "/w/1"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	stale, err := Acquire(dir, Info{Since: time.Now(), Phase: "evaluate"}, time.Hour)
	if err != nil || stale == nil || stale.Worktree != "/w/1" {
		t.Fatal(stale, err)
	}
	cur, _ := Read(dir)
	if cur.Phase != "evaluate" {
		t.Fatal(cur)
	}
}

func TestBrokenLockIsStale(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tick.lock"), []byte("{壊れた"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir, Info{Since: time.Now()}, time.Hour); err != nil {
		t.Fatal("壊れたロックを古いものとして扱えない:", err)
	}
}
