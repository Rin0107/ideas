package room

import "testing"

// /proc の中身はカーネルが書くが、形が変わっても落ちず、受け付けた値は負にならないこと。
func FuzzParse(f *testing.F) {
	f.Add(mem8G)
	f.Add(psiZero)
	f.Add("MemAvailable: -5 kB\nsome avg10=-1\n")
	f.Fuzz(func(t *testing.T, s string) {
		if v, ok := memAvailableMB(s); ok && v < 0 {
			t.Fatal(v)
		}
		if p, ok := psiSome10(s); ok && !(p >= 0) {
			t.Fatal(p)
		}
	})
}
