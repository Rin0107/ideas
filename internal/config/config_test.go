package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const good = `{"owner":"o","main_checkout":"~/m","apps_dir":"~/a","runtime_dir":"/r","ticks_per_day":6,"max_ticks_mvp":30,
"max_ticks_iteration":15,"max_attempts_per_issue":3,"lock_ttl_minutes":150,"max_consecutive_failures":3,
"site":{"repo":"o/site","url":"https://s"}}`

func write(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "factory.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadWalksUpAndExpands(t *testing.T) {
	t.Setenv("HOME", "/h")
	root := t.TempDir()
	write(t, root, good)
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := Load(sub)
	if err != nil {
		t.Fatal(err)
	}
	if c.Root != root || c.MainCheckout != "/h/m" || c.AppsDir != "/h/a" || c.RuntimeDir != "/r" || c.Site.Repo != "o/site" {
		t.Fatalf("%+v", c)
	}
}

func TestLoadErrors(t *testing.T) {
	d := t.TempDir()
	write(t, d, "{")
	if _, err := Load(d); err == nil || !strings.Contains(err.Error(), "factory.json") {
		t.Fatal("壊れた JSON を通した", err)
	}
	write(t, d, `{"owner":""}`)
	if _, err := Load(d); err == nil || !strings.Contains(err.Error(), "owner") || !strings.Contains(err.Error(), "ticks_per_day") {
		t.Fatal("足りない値を通した", err)
	}
	if _, err := Load("/"); err == nil || !strings.Contains(err.Error(), "見つからない") {
		t.Fatal("ないのに見つかった", err)
	}
}

func TestLoadWithoutWorkingDir(t *testing.T) {
	d := t.TempDir()
	gone := filepath.Join(d, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("relative"); err == nil {
		t.Fatal("今のディレクトリがないのに読めた")
	}
}

func TestExpand(t *testing.T) {
	t.Setenv("HOME", "/h")
	for in, want := range map[string]string{"~": "/h", "~/x": "/h/x", "/abs": "/abs", "~x": "~x", "": ""} {
		if got := Expand(in); got != want {
			t.Errorf("Expand(%q) = %q", in, got)
		}
	}
	t.Setenv("HOME", "")
	if got := Expand("~/x"); got != "~/x" {
		t.Fatal("ホームがないのに展開した", got)
	}
}

func TestHardProtected(t *testing.T) {
	c := &Config{ProtectedPaths: []string{"cmd/", "templates/web/.github/", "factory.json"}, ReviewablePaths: []string{"templates/web/.github/"}}
	if got := c.HardProtected(); len(got) != 2 || got[0] != "cmd/" || got[1] != "factory.json" {
		t.Fatal(got)
	}
	if got := (&Config{ProtectedPaths: []string{"a"}}).HardProtected(); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestParallelAndWIP(t *testing.T) {
	c := &Config{}
	if c.Parallel() != 1 || c.WIP() != 1 {
		t.Fatal("0 は 1")
	}
	c.MaxParallel, c.MaxWIP = 2, 3
	if c.Parallel() != 2 || c.WIP() != 3 {
		t.Fatal(c.Parallel(), c.WIP())
	}
}

func TestLimitsPolicy(t *testing.T) {
	p := Limits{}.Policy()
	if p.FiveHourMax != 70 || p.SevenDayMax != 85 || p.MaxAge != 3*time.Hour {
		t.Fatalf("既定 = %+v", p)
	}
	p = Limits{FiveHourMax: 50, SevenDayMax: 60, MaxAgeMinutes: 30}.Policy()
	if p.FiveHourMax != 50 || p.SevenDayMax != 60 || p.MaxAge != 30*time.Minute {
		t.Fatalf("指定 = %+v", p)
	}
}

func TestWebDBURL(t *testing.T) {
	if u := (WebDB{}).URL("Rin0107/kisekae-closet"); u != "" {
		t.Fatal(u)
	}
	if u := (WebDB{Subdomain: "f"}).URL("Rin0107/kisekae-closet"); u != "https://kisekae-closet.f.workers.dev/" {
		t.Fatal(u)
	}
}

// focus は文字列 1 つ ("" は主力なし) か配列 (空の要素は捨てる)。それ以外は読めない
func TestFocus(t *testing.T) {
	for in, want := range map[string]Focus{`""`: nil, `"a"`: {"a"}, `["a","","b"]`: {"a", "b"}, `[]`: {}} {
		var f Focus
		if err := json.Unmarshal([]byte(in), &f); err != nil || !slices.Equal(f, want) || (f == nil) != (want == nil) {
			t.Fatalf("%s: %#v %v", in, f, err)
		}
	}
	var f Focus
	if err := json.Unmarshal([]byte(`1`), &f); err == nil || !strings.Contains(err.Error(), "文字列か文字列の配列") {
		t.Fatal(err)
	}
	f = Focus{"a", "b", "c"}
	if !f.Has("b") || f.Has("z") || f.String() != "a・b・c" {
		t.Fatal(f)
	}
	if got := f.Next(map[string]bool{"a": true, "c": true}); got != "b" {
		t.Fatal(got)
	}
	if got := f.Next(map[string]bool{"a": true, "b": true, "c": true}); got != "" {
		t.Fatal(got)
	}
}
