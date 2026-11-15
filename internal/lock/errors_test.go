package lock

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadAndAcquireErrors(t *testing.T) {
	now := time.Now()
	// tick.lock がディレクトリ (読めない)
	d := t.TempDir()
	if err := os.Mkdir(filepath.Join(d, "tick-k.lock"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(d, "k"); err == nil {
		t.Fatal("ディレクトリを読めた")
	}
	if _, err := Acquire(d, "k", Info{Since: now}, time.Hour); err == nil {
		t.Fatal("読めないロックがあるのに取れた")
	}
	// ロックの置き場所の親がファイル
	f := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(filepath.Join(f, "x"), "k", Info{Since: now}, time.Hour); err == nil {
		t.Fatal("置き場所を作れないのに取れた")
	}
	if os.Geteuid() == 0 {
		t.Skip("root は権限で止まらない")
	}
	// 書けない場所: 古いロックを消せない・新しいロックを作れない
	ro := t.TempDir()
	if _, err := Acquire(ro, "k", Info{Since: now.Add(-2 * time.Hour)}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(ro, 0o755) })
	if _, err := Acquire(ro, "k", Info{Since: now}, time.Hour); err == nil {
		t.Fatal("古いロックを消せないのに取れた")
	}
	empty := t.TempDir()
	if err := os.Chmod(empty, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(empty, 0o755) })
	if _, err := Acquire(empty, "k", Info{Since: now}, time.Hour); err == nil {
		t.Fatal("ロックを作れないのに取れた")
	}
}
