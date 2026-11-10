package app

import (
	"strings"
	"testing"
)

// fuzzing: どんな入力でも panic せず、受け付けたものは i と r だけで上限以内。
// go test ではシード (f.Add と testdata/fuzz) だけを流す。CI の fuzz ジョブが時間を区切って新しい入力を探す。
func FuzzParseOps(f *testing.F) {
	for _, s := range []string{"", "i", "r", "iirri", "x", "i\x00", strings.Repeat("i", MaxOps+1)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		ops, err := ParseOps(s)
		if err != nil {
			return
		}
		if len(ops) != len(s) || len(ops) > MaxOps || strings.Trim(s, "ir") != "" {
			t.Fatalf("%q を受け付けた", s)
		}
		c := must(NewCounter(3))
		c.Apply(ops)
		if v := c.Value(); v < 0 || v > 3 {
			t.Fatal(v)
		}
	})
}

func FuzzRun(f *testing.F) {
	for _, a := range [][2]string{{"-max=2", "iii"}, {"-version", ""}, {"-max=-1", ""}, {"--", "x"}} {
		f.Add(a[0], a[1])
	}
	f.Fuzz(func(t *testing.T, flagArg, ops string) {
		var b strings.Builder
		_ = Run([]string{flagArg, ops}, &b) // panic しないこと
	})
}
