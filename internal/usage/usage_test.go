package usage

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func line(ts time.Time, id, req string, in, cc, cr, out int) string {
	return `{"type":"assistant","timestamp":"` + ts.Format(time.RFC3339Nano) + `","requestId":"` + req + `","message":{"id":"` + id +
		`","usage":{"input_tokens":` + itoa(in) + `,"cache_creation_input_tokens":` + itoa(cc) + `,"cache_read_input_tokens":` + itoa(cr) +
		`,"output_tokens":` + itoa(out) + `}}}` + "\n"
}

func itoa(n int) string { return strconv.Itoa(n) }

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSum(t *testing.T) {
	d := t.TempDir()
	now := time.Now()
	from, to := now.Add(-time.Hour), now.Add(time.Hour)
	// 同じ応答の塊が 2 行 (出力は後の行が多い)・別の応答・範囲の外・ユーザーの行・ID のない行 (requestId で数える・行番号で数える)
	write(t, filepath.Join(d, "-a-wakuwaku-tick", "s.jsonl"),
		line(now, "m1", "r1", 10, 100, 1000, 1)+line(now, "m1", "r1", 10, 100, 1000, 50)+
			line(now, "m2", "r2", 1, 2, 3, 4)+
			line(now.Add(-2*time.Hour), "m3", "r3", 9999, 0, 0, 0)+
			`{"type":"user","timestamp":"`+now.Format(time.RFC3339)+`","message":{"usage":{"input_tokens":5}}}`+"\n"+
			line(now, "", "r4", 1, 0, 0, 0)+
			line(now, "", "", 1, 0, 0, 0)+
			`{"usage": 壊れた行}`+"\n"+
			line(now, "m5", "r5", 1, 0, 0, 0)) // 最後の行は改行なし
	b, _ := os.ReadFile(filepath.Join(d, "-a-wakuwaku-tick", "s.jsonl"))
	write(t, filepath.Join(d, "-a-wakuwaku-tick", "s.jsonl"), strings.TrimSuffix(string(b), "\n"))
	// サブエージェントの記録・名前の合わないディレクトリ・古いファイル・jsonl でないファイル
	write(t, filepath.Join(d, "-a-wakuwaku-tick", "s", "subagents", "a.jsonl"), line(now, "m6", "r6", 0, 0, 0, 7))
	write(t, filepath.Join(d, "-home-me-src-okuos", "x.jsonl"), line(now, "m7", "r7", 500, 0, 0, 0))
	old := filepath.Join(d, "-a-wakuwaku-old", "o.jsonl")
	write(t, old, line(now, "m8", "r8", 500, 0, 0, 0))
	if err := os.Chtimes(old, now.Add(-3*time.Hour), now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(d, "-a-wakuwaku-tick", "notes.txt"), line(now, "m9", "r9", 500, 0, 0, 0))
	write(t, filepath.Join(d, "file-wakuwaku"), "")
	extra := filepath.Join(t.TempDir(), "x", "extra.jsonl")
	if err := Append(extra, now, Usage{Tokens: 20, CacheRead: 2}); err != nil {
		t.Fatal(err)
	}
	if err := Append(extra, now.Add(-2*time.Hour), Usage{Tokens: 9999}); err != nil {
		t.Fatal(err)
	}
	u, err := Sum(d, "wakuwaku", extra, from, to)
	if err != nil {
		t.Fatal(err)
	}
	// m1: 10+100+50=160 (cr 1000)、m2: 1+2+4=7 (cr 3)、r4: 1、行番号: 1、m5: 1、m6: 7、extra: 20 (cr 2)
	if want := (Usage{Tokens: 160 + 7 + 1 + 1 + 1 + 7 + 20, CacheRead: 1005}); u != want {
		t.Fatalf("got %+v want %+v", u, want)
	}
}

func TestSumMissingAndErrors(t *testing.T) {
	now := time.Now()
	if u, err := Sum(filepath.Join(t.TempDir(), "none"), "wakuwaku", "", now, now); err != nil || u != (Usage{}) {
		t.Fatal(u, err)
	}
	f := filepath.Join(t.TempDir(), "file")
	write(t, f, "")
	if _, err := Sum(f, "wakuwaku", "", now, now); err == nil {
		t.Fatal("ディレクトリでないのに通った")
	}
	d := t.TempDir()
	bad := filepath.Join(d, "-wakuwaku", "s.jsonl")
	write(t, bad, line(now, "m", "r", 1, 0, 0, 0))
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(bad); err == nil {
		t.Skip("root なので読めてしまう")
	}
	if _, err := Sum(d, "wakuwaku", "", now.Add(-time.Hour), now.Add(time.Hour)); err == nil {
		t.Fatal("読めないファイルで通った")
	}
	if _, err := Sum(t.TempDir(), "wakuwaku", d, now, now); err == nil {
		t.Fatal("extra がディレクトリなのに通った")
	}
	if _, err := Sum(t.TempDir(), "wakuwaku", bad, now, now); err == nil {
		t.Fatal("extra が読めないのに通った")
	}
	unreadable := filepath.Join(d, "-wakuwaku", "sub")
	write(t, filepath.Join(unreadable, "a.jsonl"), "")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })
	if err := os.Chmod(bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Sum(d, "wakuwaku", "", now.Add(-time.Hour), now.Add(time.Hour)); err == nil {
		t.Fatal("読めないディレクトリで通った")
	}
}

func TestAppendError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	write(t, f, "")
	if err := Append(filepath.Join(f, "x", "e.jsonl"), time.Now(), Usage{}); err == nil {
		t.Fatal("置けない場所なのに通った")
	}
	if err := Append(t.TempDir(), time.Now(), Usage{}); err == nil {
		t.Fatal("ディレクトリに書けた")
	}
}

func TestFromResult(t *testing.T) {
	text, u := FromResult(`{"type":"result","result":"{\"verdict\":\"approve\"}","usage":{"input_tokens":3,"cache_creation_input_tokens":4,"cache_read_input_tokens":5,"output_tokens":6}}`)
	if text != `{"verdict":"approve"}` || u != (Usage{Tokens: 13, CacheRead: 5}) {
		t.Fatal(text, u)
	}
	for _, s := range []string{"ただの文", `{"verdict":"approve"}`} {
		if text, u := FromResult(s); text != s || u != (Usage{}) {
			t.Fatal(text, u)
		}
	}
}

func TestScanReadError(t *testing.T) {
	// ディレクトリは開けるが読めない (読み出しの途中で落ちる)
	if err := scan(t.TempDir(), time.Now(), time.Now(), map[string]Usage{}); err == nil {
		t.Fatal("読めないのに通った")
	}
	if err := scan(filepath.Join(t.TempDir(), "none"), time.Now(), time.Now(), map[string]Usage{}); err == nil {
		t.Fatal("ないのに通った")
	}
}

func TestWeighted(t *testing.T) {
	if w := (Usage{Tokens: 5, CacheRead: 1000}).Weighted(); w != 105 {
		t.Fatal(w)
	}
}
