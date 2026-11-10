package guard

import "testing"

func TestEdges(t *testing.T) {
	c := ctx()
	if Edit("", c) != "" {
		t.Fatal("空のパスは止めない (書き込みがない)")
	}
	if Bash("cat factory.json; ; ls", c) != "" {
		t.Fatal("読むだけのコマンドの空の区切りで止めた")
	}
	if got := TouchesProtected([]string{"", "  ", "factory.json"}, c.Protected); len(got) != 1 {
		t.Fatal(got)
	}
}
