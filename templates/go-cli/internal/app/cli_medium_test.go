//go:build medium

package app

// M のテスト: 本物の実行ファイルを作って動かす (プロセスをまたぐ・ディスクを使う)。
// make cover が -tags medium で流し、実行ファイルのカバレッジ (main を含む) も 100% の計算に入れる。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestM_Binary(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "app")
	build := exec.Command("go", "build", "-cover", "-o", bin, "../../cmd/app")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, c := range []struct {
		args []string
		out  string
		code int
	}{
		{[]string{"-max", "2", "iii"}, "2\n", 0},
		{[]string{"-version"}, Version + "\n", 0},
		{[]string{"zzz"}, "", 1},
	} {
		cmd := exec.Command(bin, c.args...)
		cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverDir(t))
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != c.code || stdout.String() != c.out {
			t.Errorf("%q: code=%d out=%q err=%q", c.args, code, stdout.String(), stderr.String())
		}
	}
}

// coverDir は実行ファイルのカバレッジの置き場所 (make cover が COVER_M で渡す。なければ捨てる)。
func coverDir(t *testing.T) string {
	if d := os.Getenv("COVER_M"); d != "" {
		return d
	}
	return t.TempDir()
}
