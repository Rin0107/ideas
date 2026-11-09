// Package lock は tick が同時に 2 つ動かないようにする。
// ロックは worktree をまたいで共有する runtime_dir に置く。落ちた tick のロックは TTL で切れる。
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Info struct {
	Since    time.Time `json:"since"`
	Phase    string    `json:"phase"`
	Worktree string    `json:"worktree"`
}

func path(dir string) string { return filepath.Join(dir, "tick.lock") }

// Read は今のロックを返す (なければ nil)。
func Read(dir string) (*Info, error) {
	b, err := os.ReadFile(path(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var i Info
	if err := json.Unmarshal(b, &i); err != nil {
		return &Info{}, nil // 壊れたロックは古いものとして扱う
	}
	return &i, nil
}

// Stale はロックが TTL を過ぎているか。
func (i *Info) Stale(now time.Time, ttl time.Duration) bool {
	return i.Since.IsZero() || now.Sub(i.Since) > ttl
}

// Acquire はロックを取る。生きているロックがあれば失敗する。
// 古いロックがあれば上書きし、その情報を返す (呼び出し側が aborted と記録する)。
func Acquire(dir string, info Info, ttl time.Duration) (stale *Info, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	cur, err := Read(dir)
	if err != nil {
		return nil, err
	}
	if cur != nil {
		if !cur.Stale(info.Since, ttl) {
			return nil, fmt.Errorf("ほかの tick が動いている (%s から、%s)", cur.Since.Format(time.TimeOnly), cur.Worktree)
		}
		if err := os.Remove(path(dir)); err != nil {
			return nil, err
		}
		stale = cur
	}
	f, err := os.OpenFile(path(dir), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("ロックを取れない: %w", err)
	}
	defer f.Close()
	return stale, json.NewEncoder(f).Encode(info)
}

func Release(dir string) error {
	err := os.Remove(path(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
