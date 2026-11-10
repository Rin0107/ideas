package ideas

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Fatal("ないディレクトリを読めた")
	}
	d := t.TempDir()
	if err := os.Mkdir(filepath.Join(d, "00001-no-md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(d); err == nil {
		t.Fatal("idea.md がないのに読めた")
	}
}

func TestProblemsTitleAndSlug(t *testing.T) {
	all := []*Idea{
		{Dir: "00001-a", ID: "00001", Slug: "a", Title: "", Sections: full()},
		{Dir: "00002-a", ID: "00002", Slug: "a", Title: "別", Sections: full()},
	}
	got := strings.Join(Problems(all), "\n")
	if !strings.Contains(got, "00001-a: タイトル") || !strings.Contains(got, "00002-a: slug が 00001-a と同じ") {
		t.Fatal(got)
	}
}

func TestCandidatesLimitsCount(t *testing.T) {
	var all []*Idea
	for _, s := range []string{"a", "b", "c", "d"} {
		all = append(all, &Idea{Dir: "0000" + s, ID: "0000" + s, Slug: s})
	}
	if got := Candidates(all, map[string]bool{}, 2, "2026-11-10"); len(got) != 2 {
		t.Fatal(len(got))
	}
}

func full() map[string]string {
	m := map[string]string{}
	for _, s := range RequiredSections {
		m[s] = "x"
	}
	return m
}
