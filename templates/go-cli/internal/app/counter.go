package app

// 例: 上限つきのカウンター。仕様は spec/Counter.tla (状態の流れ) と spec/counter.smt2 (算術の性質)。
// アプリを作るときは、この例 (counter.go・counter_test.go・spec/Counter.*・spec/counter.smt2) を消して本物に置き換える。
// 形は同じにする: 仕様を書く → 実装する → 仕様の不変条件ごとに TestSpec_<名前> で実装を検査する。

import (
	"errors"
	"fmt"
)

// MaxLimit はカウンターの上限に指定できる最大値。
const MaxLimit = 1000

// MaxOps は 1 回に受け付ける操作の数の上限 (入力の大きさに上限をかける)。
const MaxOps = 10000

// ErrLimit は上限が範囲 [0, MaxLimit] の外のとき。
var ErrLimit = fmt.Errorf("上限は 0〜%d", MaxLimit)

// Counter は 0 から max までの値を持つ。仕様の変数 n に当たる。
type Counter struct{ n, max int }

// NewCounter は上限 max のカウンターを作る。仕様の Init に当たる。
func NewCounter(max int) (*Counter, error) {
	if max < 0 || max > MaxLimit {
		return nil, ErrLimit
	}
	return &Counter{max: max}, nil
}

// Inc は上限未満なら 1 増やして true。上限なら何もせず false (仕様では足踏み)。仕様の Inc に当たる。
func (c *Counter) Inc() bool {
	if c.n >= c.max {
		return false
	}
	c.n++
	return true
}

// Reset は 0 に戻す。仕様の Reset に当たる。
func (c *Counter) Reset() { c.n = 0 }

// Value は今の値。
func (c *Counter) Value() int { return c.n }

// Op は操作。
type Op byte

const (
	OpInc   Op = 'i'
	OpReset Op = 'r'
)

// ParseOps は "iir" のような文字列を操作の列にする。
func ParseOps(s string) ([]Op, error) {
	if len(s) > MaxOps {
		return nil, fmt.Errorf("操作は %d 個まで", MaxOps)
	}
	ops := make([]Op, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch Op(s[i]) {
		case OpInc, OpReset:
			ops = append(ops, Op(s[i]))
		default:
			return nil, errors.New("操作は i (増やす) と r (戻す) だけ")
		}
	}
	return ops, nil
}

// Apply は操作を順に行う。
func (c *Counter) Apply(ops []Op) {
	for _, o := range ops {
		if o == OpInc {
			c.Inc()
		} else {
			c.Reset()
		}
	}
}
