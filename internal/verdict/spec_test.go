package verdict

// spec/Factory.tla の IterationBound と spec/verdict.smt2 を、実装 (Decide) に対して PBT で確かめる。

import (
	"testing"

	"github.com/Rin0107/ideas/internal/config"
	"pgregory.net/rapid"
)

func genEval(t *rapid.T) EvalScore {
	return EvalScore{
		CoreWorks: rapid.Bool().Draw(t, "core"),
		Wakuwaku:  rapid.IntRange(1, 5).Draw(t, "w"),
		Quality:   rapid.IntRange(1, 5).Draw(t, "q"),
		Summary:   "s",
		Weakest:   "w",
	}
}

// IterationBound: ブラッシュアップは上限の回数まで (回数が尽きたら iterate を返さない)。
// あわせて verdict.smt2 の 4 つの性質 (核が動かなければ ship しない・ship なら基準以上・結論はいつも 3 つのどれか) も確かめる。
func TestSpec_IterationBound(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		e := genEval(t)
		it := rapid.IntRange(0, 5).Draw(t, "iteration")
		mx := rapid.IntRange(0, 5).Draw(t, "max")
		c := config.Evaluate{ShipMinWakuwaku: rapid.IntRange(1, 5).Draw(t, "sw"), ShipMinQuality: rapid.IntRange(1, 5).Draw(t, "sq")}
		v, why, err := Decide(e, it, mx, c)
		if err != nil || why == "" {
			t.Fatal(err)
		}
		switch v {
		case "iterate":
			if it >= mx || !e.CoreWorks {
				t.Fatalf("回数 %d/%d・核 %v で iterate", it, mx, e.CoreWorks)
			}
		case "ship":
			if !e.CoreWorks || e.Wakuwaku < c.ShipMinWakuwaku || e.Quality < c.ShipMinQuality {
				t.Fatalf("基準に届かないのに ship: %+v %+v", e, c)
			}
		case "drop":
		default:
			t.Fatalf("知らない結論 %q", v)
		}
	})
}
