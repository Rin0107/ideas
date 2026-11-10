package ideas

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// fuzzing: どんな idea.md でも panic せず、読めたら ID と slug はフォルダ名から決まる (中身では変わらない)。
func FuzzParse(f *testing.F) {
	f.Add("# タイトル\n\n## 概要\nx\n## 体験の核\ny\n## 履歴\n- wk:never\n")
	f.Add("")
	f.Add("##\n#\n## 履歴")
	f.Fuzz(func(t *testing.T, body string) {
		root := t.TempDir()
		dir := filepath.Join(root, "00007-fuzz-me")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "idea.md")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		all, err := Load(root)
		if err != nil {
			return
		}
		if len(all) != 1 {
			t.Fatal(len(all))
		}
		if i := all[0]; i.ID != "00007" || i.Slug != "fuzz-me" || i.Dir != "00007-fuzz-me" {
			t.Fatalf("%+v", all[0])
		}
	})
}

// fuzzing: どんな INDEX.md でも panic せず、結果はいつも並んでいる (毎回同じ)。
func FuzzIndexProblems(f *testing.F) {
	f.Add("- 00001 a — x\n- 00002 b — y\n")
	f.Add("- 00001 z — \n- 0000 — ")
	f.Fuzz(func(t *testing.T, index string) {
		all := []*Idea{{Dir: "00001-a", ID: "00001", Slug: "a"}, {Dir: "00002-b", ID: "00002", Slug: "b"}}
		got := IndexProblems(index, all)
		if !slices.IsSorted(got) {
			t.Fatal(got)
		}
	})
}
