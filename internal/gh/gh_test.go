package gh

import (
	"strings"
	"testing"
)

func TestSetupAuth(t *testing.T) {
	d := t.TempDir()
	if _, err := Run(d, "git", "init", "-q"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // 何度やっても同じになる
		if err := SetupAuth(d); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Run(d, "git", "config", "--local", "--get-all", "credential.helper")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(strings.TrimRight(out, "\n"), "\n"); len(got) != 2 || got[0] != "" || got[1] != "!gh auth git-credential" {
		t.Fatalf("%q", got)
	}
}
