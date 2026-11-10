package state

// spec/Factory.tla の FinishedIsTransient を、実装 (Portfolio.Active) に対して PBT で確かめる。

import (
	"fmt"
	"testing"

	"pgregory.net/rapid"
)

// FinishedIsTransient: 公開・破棄したアプリは「作っているもの」にならない (すぐ次のアプリに移れる)。
func TestSpec_FinishedIsTransient(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pf := &Portfolio{}
		n := rapid.IntRange(0, 10).Draw(t, "n")
		for i := 0; i < n; i++ {
			st := rapid.SampledFrom([]string{Selected, Building, Evaluating, Iterating, Shipped, Dropped}).Draw(t, fmt.Sprint("s", i))
			pf.Projects = append(pf.Projects, &Project{Idea: fmt.Sprint(i), Status: st})
		}
		a := pf.Active()
		if a != nil && (a.Status == Shipped || a.Status == Dropped) {
			t.Fatalf("終わったアプリ %s を作っているものとして返した", a.Status)
		}
		anyActive := false
		for _, p := range pf.Projects {
			anyActive = anyActive || p.Active()
		}
		if anyActive != (a != nil) {
			t.Fatalf("作っているものがあるのに返さない (または逆)")
		}
	})
}
