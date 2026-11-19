package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTasksRoundTrip(t *testing.T) {
	root := t.TempDir()
	ts, err := LoadTasks(root, "Tester/alpha")
	if err != nil || len(ts.Tasks) != 0 || ts.Get(3) != nil {
		t.Fatal(ts, err)
	}
	ts.Put(&Task{Issue: 3, Title: "a", Checks: []Check{{Name: "n", Run: "true"}}})
	if err := SaveTasks(root, "Tester/alpha", ts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "state", "tasks", "alpha.json")); err != nil {
		t.Fatal(err)
	}
	back, err := LoadTasks(root, "Tester/alpha")
	if err != nil || back.Get(3) == nil || back.Get(3).Checks[0].Run != "true" || back.Get(4) != nil {
		t.Fatal(back, err)
	}
	if err := os.WriteFile(filepath.Join(root, "state", "tasks", "alpha.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ts, err := LoadTasks(root, "Tester/alpha"); err == nil || ts.Tasks == nil {
		t.Fatal("壊れた記録を読めた", ts)
	}
}

func TestTaskAllowed(t *testing.T) {
	tk := &Task{Paths: []string{"src/**", "index.html", "public/*.css"}, Tests: map[string]string{"src/x.test.ts": "h"}}
	for f, want := range map[string]bool{
		"src/a.ts": true, "src/deep/b.ts": true, "index.html": true, "public/a.css": true,
		"src/x.test.ts": false, "srcx/a.ts": false, "src": false, "public/a/b.css": false, "README.md": false,
	} {
		if got := tk.Allowed(f); got != want {
			t.Errorf("%s: %v", f, got)
		}
	}
}

func TestPlanNight(t *testing.T) {
	for at, want := range map[string]string{
		"2026-11-19T23:10:00+09:00": "2026-11-19", "2026-11-20T00:40:00+09:00": "2026-11-19",
		"2026-11-20T11:59:00+09:00": "2026-11-19", "2026-11-20T12:00:00+09:00": "2026-11-20",
	} {
		tm, err := time.Parse(time.RFC3339, at)
		if err != nil {
			t.Fatal(err)
		}
		if got := PlanNight(tm); got != want {
			t.Errorf("%s: %s", at, got)
		}
	}
}
