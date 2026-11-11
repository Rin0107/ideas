package main

import (
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	var b strings.Builder
	if run(nil, &b) != 2 || !strings.Contains(b.String(), "使い方: wk <コマンド>") || !strings.Contains(b.String(), "guard-bash") {
		t.Fatal(b.String())
	}
	b.Reset()
	if run([]string{"nope"}, &b) != 2 {
		t.Fatal("知らないコマンド")
	}
	f := newFactory(t)
	if code, _ := captureCode(t, func() int { return run([]string{"check"}, &b) }); code != 0 {
		t.Fatal(b.String())
	}
	b.Reset()
	if run([]string{"done", "--result", "x"}, &b) != 1 || !strings.Contains(b.String(), "wk done: --result は ok・retry・fix・human のどれか") {
		t.Fatal(b.String())
	}
	f.write("STOP", "x")
	b.Reset()
	if run([]string{"due"}, &b) != 1 || !strings.Contains(b.String(), "STOP") {
		t.Fatal(b.String())
	}
	stdin(t, hook(f.root, "command", "gh repo delete Tester/ideas"))
	if run([]string{"guard-bash"}, &b) != 2 {
		t.Fatal("フックの拒否は終了コード 2")
	}
}

func captureCode(t *testing.T, fn func() int) (int, string) {
	var code int
	out, _ := capture(t, func() error { code = fn(); return nil })
	return code, out
}
