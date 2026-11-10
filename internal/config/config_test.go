package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
