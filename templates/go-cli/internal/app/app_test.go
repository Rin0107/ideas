package app

import (
	"errors"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var b strings.Builder
	if err := Run([]string{"-version"}, &b); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(b.String()) != Version {
		t.Fatalf("got %q", b.String())
	}
}

func TestRun(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
		ok   bool
	}{
		{nil, "0\n", true},
		{[]string{"iii"}, "3\n", true},
		{[]string{"-max", "2", "iiiri"}, "1\n", true},
		{[]string{"-max", "-1"}, "", false},
		{[]string{"x"}, "", false},
		{[]string{"i", "extra"}, "", false},
		{[]string{"-nope"}, "", false},
	} {
		var b strings.Builder
		err := Run(c.args, &b)
		if (err == nil) != c.ok || b.String() != c.want {
			t.Errorf("Run(%q) = %q, %v", c.args, b.String(), err)
		}
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("書けない") }

func TestRunReportsWriteErrors(t *testing.T) {
	for _, args := range [][]string{{"-version"}, {"i"}} {
		if err := Run(args, failWriter{}); err == nil {
			t.Errorf("Run(%q) が書き込みの失敗を返さない", args)
		}
	}
}

func TestMain_ExitCodes(t *testing.T) {
	var out, errb strings.Builder
	if code := Main([]string{"ii"}, &out, &errb); code != 0 || out.String() != "2\n" || errb.Len() != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errb.String())
	}
	out.Reset()
	if code := Main([]string{"x"}, &out, &errb); code != 1 || out.Len() != 0 || !strings.Contains(errb.String(), "{{NAME}}:") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errb.String())
	}
}
