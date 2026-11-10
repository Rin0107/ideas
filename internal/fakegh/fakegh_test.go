package fakegh

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func run(args ...string) (string, int) {
	out, err := exec.Command("gh", args...).Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), ee.ExitCode()
	}
	return string(out), 0
}

func TestFake(t *testing.T) {
	f := Install(t, Rule{Match: `^repo view o/a`, Out: "a\n"}, Rule{Match: `^repo view`, Out: "nope", Code: 1})
	if out, code := run("repo", "view", "o/a"); out != "a\n" || code != 0 {
		t.Fatal(out, code)
	}
	if out, code := run("repo", "view", "o/b"); out != "nope" || code != 1 {
		t.Fatal("先の決まりが優先されない", out, code)
	}
	if _, code := run("pr", "list"); code != 97 {
		t.Fatal("決まりのない呼び出しが 97 でない", code)
	}
	f.Add(t, Rule{Match: `^pr list`, Out: "[]"})
	if out, _ := run("pr", "list"); out != "[]" {
		t.Fatal(out)
	}
	if got := strings.Join(f.Calls(), "|"); got != "repo view o/a|repo view o/b|pr list|pr list" {
		t.Fatal(got)
	}
	if !f.Called("view o/b") || f.Called("delete") {
		t.Fatal("Called")
	}
}

func TestCallsEmpty(t *testing.T) {
	if Install(t).Calls() != nil {
		t.Fatal("呼ばれていないのに記録がある")
	}
}

type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Helper()           {}
func (f *fatalTB) Fatal(args ...any) { f.msg = fmt.Sprint(args...) }

func TestMustReportsErrors(t *testing.T) {
	tb := &fatalTB{TB: t}
	must(tb, errors.New("壊れた"))
	if tb.msg != "壊れた" {
		t.Fatal(tb.msg)
	}
	must(tb, nil)
}
