// Package fakegh はテスト用の偽物の gh。決めた引数に決めた出力を返し、呼ばれた引数を記録する。
// 本物の GitHub に触れずに、gh を呼ぶコードの全部の道 (成功・失敗) を通すために使う。
package fakegh

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Rule は「引数 (空白でつないだもの) が正規表現 Match に合えば、Out を出して終了コード Code で終わる」。
// 先に足したものが優先。どれにも合わなければ終了コード 97 (決まりのない呼び出しはテストの書き漏れ)。
type Rule struct {
	Match string
	Out   string
	Code  int
}

// Fake は PATH の先頭に置いた偽物の gh。
type Fake struct {
	dir string
	n   int
}

const script = `#!/bin/sh
args="$*"
printf '%%s\n' "$args" >> %q
for re in %q/r*.re; do
  [ -e "$re" ] || continue
  if printf '%%s' "$args" | grep -Eq -f "$re"; then
    b=${re%%.re}
    cat "$b.out"
    exit "$(cat "$b.code")"
  fi
done
echo "fakegh: 決まりがない: $args" >&2
exit 97
`

// Install は偽物の gh を PATH の先頭に置き、決まりを足す。
func Install(t testing.TB, rules ...Rule) *Fake {
	t.Helper()
	f := &Fake{dir: t.TempDir()}
	sh := fmt.Sprintf(script, filepath.Join(f.dir, "calls.log"), f.dir)
	must(t, os.WriteFile(filepath.Join(f.dir, "gh"), []byte(sh), 0o755))
	t.Setenv("PATH", f.dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	f.Add(t, rules...)
	return f
}

// Add は決まりを後ろに足す (前の決まりのほうが優先)。
func (f *Fake) Add(t testing.TB, rules ...Rule) {
	t.Helper()
	for _, r := range rules {
		f.n++
		b := filepath.Join(f.dir, fmt.Sprintf("r%04d", f.n))
		must(t, os.WriteFile(b+".re", []byte(r.Match+"\n"), 0o644))
		must(t, os.WriteFile(b+".out", []byte(r.Out), 0o644))
		must(t, os.WriteFile(b+".code", []byte(fmt.Sprint(r.Code)), 0o644))
	}
}

// Calls は呼ばれた引数の一覧 (呼ばれた順)。
func (f *Fake) Calls() []string {
	b, err := os.ReadFile(filepath.Join(f.dir, "calls.log"))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// Called は、正規表現ではなく部分文字列 sub を含む呼び出しがあったか。
func (f *Fake) Called(sub string) bool {
	for _, c := range f.Calls() {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
