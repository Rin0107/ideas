package ideas

import (
	"os"
	"path/filepath"
	"testing"
)

const good = `# 正規表現クロスワード — 毎朝一問

## 概要
a
## わくわくポイント
b
## シーン
c
## 体験の核
d
## ターゲット
e
## マネタイズ
f
## 差別化ポイント
g
`

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name, "idea.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProblemsCandidates(t *testing.T) {
	d := t.TempDir()
	write(t, d, "idea-template", "# x")
	write(t, d, "00001-regex-crossword", good)
	write(t, d, "00002-dup-title", good)                 // タイトル重複
	write(t, d, "00003-missing", "# 別のもの\n\n## 概要\nx\n") // 見出し不足
	write(t, d, "00004-never", good[:0]+"# 捨てた\n"+good[len("# 正規表現クロスワード — 毎朝一問\n"):]+"## 履歴\n- wk:never\n")
	write(t, d, "Bad_Name", good)

	all, err := Load(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("len %d", len(all))
	}
	probs := Problems(all)
	if len(probs) < 3 {
		t.Fatalf("problems: %v", probs)
	}
	if NextID(all) != "00005" {
		t.Fatal(NextID(all))
	}
	tried := map[string]bool{"00001-regex-crossword": true}
	c1 := Candidates(all, tried, 10, "2026-11-10")
	c2 := Candidates(all, tried, 10, "2026-11-10")
	if len(c1) != 2 { // 00002, 00003 (00001 は着手済み、00004 は never、Bad_Name は形が不正)
		t.Fatalf("candidates %d", len(c1))
	}
	for i := range c1 {
		if c1[i].Dir != c2[i].Dir {
			t.Fatal("同じ seed で順番が変わった")
		}
	}
	if Untried(all, tried) != 2 {
		t.Fatal(Untried(all, tried))
	}
}
