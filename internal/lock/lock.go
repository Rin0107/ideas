// Package lock は tick の実行どうしがぶつからないようにする。
// 実行 (run) ごとにロックを持ち (key は tick の worktree の名前)、何を進めているか (claim) を書く。
// ほかの実行は、そこに書かれたアプリ・段を選ばない (docs/DESIGN-v3.md §8)。落ちた実行のロックは TTL で切れる。
// 持ち主のプロセス (tick の claude) が分かれば、それが終わったロックも古いとみなす (2026-11-24、03:00 の tick が落ち 4 時間止まった)。
// Hold は状態の clone に同時に 1 つのコマンドしか書かないようにする排他ロック (flock)。
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Info struct {
	Since    time.Time `json:"since"`
	Phase    string    `json:"phase"`
	Worktree string    `json:"worktree"`
	// Repo と Idea は、この実行が進めているアプリ (ほかの実行は選ばない)
	Repo string `json:"repo,omitempty"`
	Idea string `json:"idea,omitempty"`
	// Pid と Started は持ち主のプロセスと、その開始時刻 (/proc の starttime。番号の使い回しと見分ける)。0 なら TTL だけで判断する
	Pid     int    `json:"pid,omitempty"`
	Started string `json:"started,omitempty"`
}

const prefix, suffix = "tick-", ".lock"

// Path は key のロックのファイル。
func Path(dir, key string) string { return filepath.Join(dir, prefix+key+suffix) }

// Read は key のロックを返す (なければ nil)。
func Read(dir, key string) (*Info, error) {
	b, err := os.ReadFile(Path(dir, key))
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

// List は全部の実行のロック (key → 中身)。
func List(dir string) (map[string]*Info, error) {
	paths, _ := filepath.Glob(filepath.Join(dir, prefix+"*"+suffix))
	sort.Strings(paths)
	out := map[string]*Info{}
	for _, p := range paths {
		key := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), prefix), suffix)
		i, err := Read(dir, key)
		if err != nil {
			return nil, err
		}
		out[key] = i
	}
	return out, nil
}

// Stale はロックが TTL を過ぎているか、持ち主のプロセスがもういないか。
func (i *Info) Stale(now time.Time, ttl time.Duration) bool {
	return i.Since.IsZero() || now.Sub(i.Since) > ttl || (i.Pid > 0 && !Alive(i.Pid, i.Started))
}

// ProcRoot は /proc の場所 (テストで差し替える)。
var ProcRoot = "/proc"

// stat は pid の名前・親・開始時刻 (/proc/<pid>/stat)。読めなければ ok=false
func stat(pid int) (name string, ppid int, started string, ok bool) {
	b, err := os.ReadFile(filepath.Join(ProcRoot, strconv.Itoa(pid), "stat"))
	s := string(b)
	l, r := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if err != nil || l < 0 || r < l {
		return "", 0, "", false
	}
	// ) のあとは 3 番目 (state) から。ppid は 4 番目、starttime は 22 番目
	f := strings.Fields(s[r+1:])
	if len(f) < 20 {
		return "", 0, "", false
	}
	ppid, _ = strconv.Atoi(f[1])
	return s[l+1 : r], ppid, f[19], true
}

// Alive は pid のプロセスが、started に始めたまま生きているか。
func Alive(pid int, started string) bool {
	_, _, s, ok := stat(pid)
	return ok && (started == "" || s == started)
}

// Owner は from から親をたどり、名前が name の最初のプロセス (番号と開始時刻)。なければ 0
func Owner(from int, name string) (int, string) {
	for pid, n := from, 0; pid > 1 && n < 64; n++ {
		comm, ppid, started, ok := stat(pid)
		if !ok {
			return 0, ""
		}
		if comm == name {
			return pid, started
		}
		pid = ppid
	}
	return 0, ""
}

// Acquire は key のロックを取る。生きているロックがあれば失敗する。
// 古いロックがあれば上書きし、その情報を返す (呼び出し側が aborted と記録する)。
func Acquire(dir, key string, info Info, ttl time.Duration) (stale *Info, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	cur, err := Read(dir, key)
	if err != nil {
		return nil, err
	}
	if cur != nil {
		if !cur.Stale(info.Since, ttl) {
			return nil, fmt.Errorf("ほかの tick が動いている (%s から、%s)", cur.Since.Format(time.TimeOnly), cur.Worktree)
		}
		if err := os.Remove(Path(dir, key)); err != nil {
			return nil, err
		}
		stale = cur
	}
	f, err := os.OpenFile(Path(dir, key), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("ロックを取れない: %w", err)
	}
	defer f.Close()
	return stale, json.NewEncoder(f).Encode(info)
}

// Claim は key のロックに、進めているアプリを書き足す (select でアイディアが決まったとき)。
func Claim(dir, key, repo, idea string) error {
	cur, err := Read(dir, key)
	if err != nil {
		return err
	}
	if cur == nil {
		return errors.New("ロックがない (wk next を先に実行する)")
	}
	cur.Repo, cur.Idea = repo, idea
	b, _ := json.Marshal(cur)
	return os.WriteFile(Path(dir, key), b, 0o644)
}

func Release(dir, key string) error {
	err := os.Remove(Path(dir, key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Hold は path の排他ロック (flock) を取り、外す関数を返す。wait のあいだに取れなければ失敗する。
// 落ちたプロセスの flock は OS が外すので、残らない。
func Hold(path string, wait, poll time.Duration) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil // 閉じれば flock も外れる
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("状態のロック (%s) を取れない: %w", path, err)
		}
		time.Sleep(poll)
	}
}
