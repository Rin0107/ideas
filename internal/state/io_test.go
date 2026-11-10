package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mk(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func file(t *testing.T, p, body string) {
	t.Helper()
	mk(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMustJSONPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("panic しない")
		}
	}()
	mustJSON(nil, errors.New("x"))
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := SavePortfolio(root, &Portfolio{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "state", "portfolio.json")); !strings.Contains(string(b), `"projects": []`) {
		t.Fatal("空のときも [] で書く", string(b))
	}
	if err := SaveMeta(root, &Meta{LastIntake: "2026-11-10"}); err != nil {
		t.Fatal(err)
	}
	if m, err := LoadMeta(root); err != nil || m.LastIntake != "2026-11-10" {
		t.Fatal(m, err)
	}
	if pf, err := LoadPortfolio(t.TempDir()); err != nil || len(pf.Projects) != 0 {
		t.Fatal("ないときは空", pf, err)
	}
}

func TestReadWriteErrors(t *testing.T) {
	d := t.TempDir()
	// 読めない (ディレクトリ)・壊れた JSON
	mk(t, filepath.Join(d, "dir.json"))
	if readJSON(filepath.Join(d, "dir.json"), &Meta{}) == nil {
		t.Fatal("ディレクトリを読めた")
	}
	file(t, filepath.Join(d, "bad.json"), "{")
	if err := readJSON(filepath.Join(d, "bad.json"), &Meta{}); err == nil || !strings.Contains(err.Error(), "bad.json") {
		t.Fatal(err)
	}
	// 書けない: 親がファイル・一時ファイルの場所がディレクトリ・置き換え先が中身のあるディレクトリ
	file(t, filepath.Join(d, "f"), "")
	if writeJSON(filepath.Join(d, "f", "x.json"), 1) == nil {
		t.Fatal("親がファイルなのに書けた")
	}
	mk(t, filepath.Join(d, "t.json.tmp"))
	if writeJSON(filepath.Join(d, "t.json"), 1) == nil {
		t.Fatal("一時ファイルが書けないのに成功")
	}
	file(t, filepath.Join(d, "full.json", "x"), "")
	if writeJSON(filepath.Join(d, "full.json"), 1) == nil {
		t.Fatal("置き換え先がディレクトリなのに成功")
	}
	// runtime.json が壊れている
	file(t, filepath.Join(d, "rt", "runtime.json"), "{")
	if _, err := LoadRuntime(filepath.Join(d, "rt"), time.Now()); err == nil {
		t.Fatal("壊れた runtime を読めた")
	}
}

func TestTicksErrors(t *testing.T) {
	// state がファイル
	r1 := t.TempDir()
	file(t, filepath.Join(r1, "state"), "")
	if AppendTick(r1, Tick{}) == nil {
		t.Fatal("state がファイルなのに書けた")
	}
	// ticks.jsonl がディレクトリ
	r2 := t.TempDir()
	mk(t, filepath.Join(r2, "state", "ticks.jsonl"))
	if AppendTick(r2, Tick{}) == nil {
		t.Fatal("ticks.jsonl がディレクトリなのに書けた")
	}
	if _, err := ReadTicks(r2); err == nil {
		t.Fatal("ディレクトリを読めた")
	}
	// 空行は飛ばし、壊れた行は行番号つきで止める
	r3 := t.TempDir()
	file(t, filepath.Join(r3, "state", "ticks.jsonl"), `{"phase":"select","result":"ok"}`+"\n\n{\n")
	if _, err := ReadTicks(r3); err == nil || !strings.Contains(err.Error(), "ticks.jsonl:3") {
		t.Fatal(err)
	}
	// 1 行が長すぎる
	r4 := t.TempDir()
	file(t, filepath.Join(r4, "state", "ticks.jsonl"), strings.Repeat("x", 70*1024)+"\n")
	if _, err := ReadTicks(r4); err == nil {
		t.Fatal("長すぎる行を読めた")
	}
	// 読む権限がない
	r5 := t.TempDir()
	p := filepath.Join(r5, "state", "ticks.jsonl")
	file(t, p, "")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTicks(r5); err == nil && os.Geteuid() != 0 {
		t.Fatal("読めないのに読めた")
	}
}

func TestReadTicksMissingAndEmptyIdea(t *testing.T) {
	if ts, err := ReadTicks(t.TempDir()); ts != nil || err != nil {
		t.Fatal("ないときは空", ts, err)
	}
	errs := (&Portfolio{Projects: []*Project{{Status: Building, Started: "2026-11-10"}}}).Validate()
	if len(errs) != 1 || !strings.Contains(errs[0], "idea が空") {
		t.Fatal(errs)
	}
}
