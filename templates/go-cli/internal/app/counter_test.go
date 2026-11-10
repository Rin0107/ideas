package app

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// ---- 境界値: 範囲の端と、その 1 つ外 ----

func TestNewCounterBoundaries(t *testing.T) {
	for _, c := range []struct {
		max int
		ok  bool
	}{{-1, false}, {0, true}, {1, true}, {MaxLimit - 1, true}, {MaxLimit, true}, {MaxLimit + 1, false}} {
		_, err := NewCounter(c.max)
		if (err == nil) != c.ok {
			t.Errorf("NewCounter(%d): err=%v", c.max, err)
		}
	}
}

func TestIncStopsExactlyAtMax(t *testing.T) {
	c := must(NewCounter(2))
	for i, want := range []bool{true, true, false, false} {
		if got := c.Inc(); got != want {
			t.Fatalf("%d 回目の Inc = %v", i+1, got)
		}
	}
	if c.Value() != 2 {
		t.Fatal(c.Value())
	}
}

func TestParseOpsBoundaries(t *testing.T) {
	if ops, err := ParseOps(""); err != nil || len(ops) != 0 {
		t.Fatal("空を受け付けない", err)
	}
	if _, err := ParseOps(strings.Repeat("i", MaxOps)); err != nil {
		t.Fatal("上限ちょうどを受け付けない", err)
	}
	if _, err := ParseOps(strings.Repeat("i", MaxOps+1)); err == nil {
		t.Fatal("上限を超えたものを通した")
	}
	for _, bad := range []string{"x", "ii ", "I", "iR"} {
		if _, err := ParseOps(bad); err == nil {
			t.Errorf("%q を通した", bad)
		}
	}
}

// ---- 仕様と同じ不変条件を、実装に対して確かめる (spec/Counter.cfg の INVARIANT ごとに 1 つ) ----

// model は spec/Counter.tla をそのまま写したもの (Inc は n < Max のときだけ進む)。
func model(n, max int, o Op) int {
	if o == OpInc && n < max {
		return n + 1
	}
	if o == OpReset {
		return 0
	}
	return n
}

// TestSpec_TypeOK は全件ループ: 上限 0〜4 と、長さ 8 までの全部の操作列で、実装が仕様と同じ状態を通り、
// どの状態でも TypeOK (0 <= n <= Max) を満たすことを確かめる。
func TestSpec_TypeOK(t *testing.T) {
	var all func(prefix []Op, depth int, visit func([]Op))
	all = func(prefix []Op, depth int, visit func([]Op)) {
		visit(prefix)
		if depth == 0 {
			return
		}
		for _, o := range []Op{OpInc, OpReset} {
			all(append(prefix[:len(prefix):len(prefix)], o), depth-1, visit)
		}
	}
	checked := 0
	for max := 0; max <= 4; max++ {
		all(nil, 8, func(ops []Op) {
			c, n := must(NewCounter(max)), 0
			for _, o := range ops {
				c.Apply([]Op{o})
				n = model(n, max, o)
				if c.Value() != n || c.Value() < 0 || c.Value() > max {
					t.Fatalf("max=%d ops=%q: 実装 %d、仕様 %d", max, ops, c.Value(), n)
				}
			}
			checked++
		})
	}
	if checked != 5*511 {
		t.Fatalf("全件を回っていない: %d", checked)
	}
}

// TestSpec_Bounded は PBT: 大きな上限と長い操作列をランダムに作り、実装と仕様を突き合わせる。
func TestSpec_Bounded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		max := rapid.IntRange(0, MaxLimit).Draw(t, "max")
		c, n := must(NewCounter(max)), 0
		t.Repeat(map[string]func(*rapid.T){
			"inc": func(t *rapid.T) {
				moved := c.Inc()
				if moved != (n < max) {
					t.Fatalf("Inc=%v n=%d max=%d", moved, n, max)
				}
				n = model(n, max, OpInc)
			},
			"reset": func(t *rapid.T) { c.Reset(); n = 0 },
			"": func(t *rapid.T) {
				if c.Value() != n || c.Value() > max {
					t.Fatalf("実装 %d、仕様 %d (max %d)", c.Value(), n, max)
				}
			},
		})
	})
}

// PBT: ParseOps は受け付けた文字列をそのまま操作に写す (往復で同じ)。
func TestParseOpsRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringOfN(rapid.SampledFrom([]rune{'i', 'r'}), 0, 64, -1).Draw(t, "s")
		ops, err := ParseOps(s)
		if err != nil {
			t.Fatal(err)
		}
		b := make([]byte, len(ops))
		for i, o := range ops {
			b[i] = byte(o)
		}
		if string(b) != s {
			t.Fatalf("%q → %q", s, b)
		}
	})
}
