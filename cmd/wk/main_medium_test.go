//go:build medium

package main

// M のテスト: 本物の wk の実行ファイルを作って動かす (main を含めてカバレッジに入る)。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestM_Binary(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "wk")
	build := exec.Command("go", "build", "-cover", "-coverpkg=github.com/Rin0107/ideas/...", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	f := newFactory(t)
	cover := os.Getenv("COVER_M")
	if cover == "" {
		cover = t.TempDir()
	}
	for _, c := range []struct {
		args []string
		code int
		out  string
	}{
		{nil, 2, "使い方"},
		{[]string{"check"}, 0, "ok (アイディア 3 件"},
		{[]string{"done", "--result", "x"}, 1, "ok か fail"},
	} {
		cmd := exec.Command(bin, c.args...)
		cmd.Dir = f.root
		cmd.Env = append(os.Environ(), "GOCOVERDIR="+cover)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		if code != c.code || !strings.Contains(string(out), c.out) {
			t.Errorf("wk %q: code=%d\n%s", c.args, code, out)
		}
	}
}
