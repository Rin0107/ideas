// Package usage は工場の Claude が使ったトークンを数える (docs/DESIGN-v3.md §4)。
// 工場の本当の予算は tick の数ではなく、Claude の利用上限 (トークン)。数えなければ守れない。
package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Usage はトークンの数。Tokens は入力 + キャッシュ作成 + 出力、CacheRead はキャッシュの読み出し (安いので別に数える)。
type Usage struct {
	Tokens    int `json:"tokens"`
	CacheRead int `json:"cache_read"`
}

func (u *Usage) Add(o Usage) { u.Tokens += o.Tokens; u.CacheRead += o.CacheRead }

// Weighted は利用上限に効く量の目安。キャッシュの読み出しは 1/10 で数える (安いが、量がけた違いに多い。2026-11-12 は 5,700 万)
func (u Usage) Weighted() int { return u.Tokens + u.CacheRead/10 }

type record struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Usage *struct {
			Input       int `json:"input_tokens"`
			CacheCreate int `json:"cache_creation_input_tokens"`
			CacheRead   int `json:"cache_read_input_tokens"`
			Output      int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// Sum は projects (~/.claude/projects) の下の、名前に match を含むディレクトリにある会話の記録 (*.jsonl。サブエージェントの記録も)
// から、[from, to) のトークンを足す。同じ応答は 1 回だけ数える (記録は内容の塊ごとに同じ応答を繰り返し、最後の行の出力が一番多い)。
// extra は claude -p の記録 (Append が書く 1 行 1 回の JSON)。なくてもよい。
func Sum(projects, match, extra string, from, to time.Time) (Usage, error) {
	seen := map[string]Usage{}
	ents, err := os.ReadDir(projects)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Usage{}, err
	}
	for _, d := range ents {
		if !d.IsDir() || !strings.Contains(d.Name(), match) {
			continue
		}
		err := filepath.WalkDir(filepath.Join(projects, d.Name()), func(p string, de fs.DirEntry, err error) error {
			if err != nil || de.IsDir() || !strings.HasSuffix(p, ".jsonl") {
				return err
			}
			if info, err := de.Info(); err != nil || info.ModTime().Before(from) {
				return err
			}
			return scan(p, from, to, seen)
		})
		if err != nil {
			return Usage{}, err
		}
	}
	var u Usage
	for _, v := range seen {
		u.Add(v)
	}
	x, err := sumExtra(extra, from, to)
	u.Add(x)
	return u, err
}

func scan(path string, from, to time.Time, seen map[string]Usage) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for n := 0; ; n++ {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && strings.Contains(string(line), `"usage"`) {
			var rec record
			if json.Unmarshal(line, &rec) == nil && rec.Type == "assistant" && rec.Message.Usage != nil &&
				!rec.Timestamp.Before(from) && rec.Timestamp.Before(to) {
				key := rec.Message.ID
				if key == "" {
					key = rec.RequestID
				}
				if key == "" {
					key = path + "#" + strconv.Itoa(n)
				}
				m := rec.Message.Usage
				u := Usage{Tokens: m.Input + m.CacheCreate + m.Output, CacheRead: m.CacheRead}
				if old, ok := seen[key]; !ok || u.Tokens > old.Tokens {
					seen[key] = u
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type extraLine struct {
	TS time.Time `json:"ts"`
	Usage
}

// Append は claude -p の 1 回分を extra に足す (claude -p は会話の記録を残さないので、wk が自分で書く)。
func Append(extra string, ts time.Time, u Usage) error {
	if err := os.MkdirAll(filepath.Dir(extra), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(extra, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(extraLine{TS: ts, Usage: u})
	_, err = f.Write(append(b, '\n'))
	return err
}

func sumExtra(extra string, from, to time.Time) (Usage, error) {
	var u Usage
	f, err := os.Open(extra)
	if errors.Is(err, fs.ErrNotExist) || extra == "" {
		return u, nil
	}
	if err != nil {
		return u, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l extraLine
		if json.Unmarshal(sc.Bytes(), &l) == nil && !l.TS.Before(from) && l.TS.Before(to) {
			u.Add(l.Usage)
		}
	}
	return u, sc.Err()
}

// FromResult は claude -p --output-format json の出力から、本文と使ったトークンを取り出す。
// JSON でなければ (古い claude・テストの偽物) 出力をそのまま本文にする。
func FromResult(out string) (string, Usage) {
	var r struct {
		Result *string `json:"result"`
		Usage  struct {
			Input       int `json:"input_tokens"`
			CacheCreate int `json:"cache_creation_input_tokens"`
			CacheRead   int `json:"cache_read_input_tokens"`
			Output      int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &r) != nil || r.Result == nil {
		return out, Usage{}
	}
	m := r.Usage
	return *r.Result, Usage{Tokens: m.Input + m.CacheCreate + m.Output, CacheRead: m.CacheRead}
}
