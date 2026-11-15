package verdict

import (
	"strings"
	"testing"

	"github.com/Rin0107/ideas/internal/config"
)

func sc(idea string, w, l, f, o, fy int) SelectScore {
	return SelectScore{Idea: idea, Wakuwaku: w, Light: l, Free: f, Own: o, Findy: fy, Stack: "web", Reason: "x", DataPlan: "なし"}
}

func TestChooseIdea(t *testing.T) {
	c := config.Select{MinFree: 3, MinLight: 3}
	allowed := map[string]bool{"a": true, "b": true, "c": true, "d": true, "e": true, "f": true}
	hw := sc("d", 5, 5, 5, 5, 5)
	hw.Hardware = true
	sens := sc("e", 5, 5, 5, 5, 5)
	sens.Sensitive = true
	srv := sc("f", 5, 5, 5, 5, 5)
	srv.NeedsServer = true
	got, log, err := ChooseIdea([]SelectScore{
		sc("a", 4, 4, 4, 4, 4), // 20
		sc("b", 5, 5, 2, 5, 5), // 無料で足切り
		sc("c", 5, 4, 4, 4, 3), // 20 だがわくわくが上
		hw,                     // 物が要る
		sens,                   // 重要な情報が要る
		srv,                    // サーバーが要る
	}, allowed, c)
	if err != nil {
		t.Fatal(err)
	}
	if got.Idea != "c" {
		t.Fatalf("got %s", got.Idea)
	}
	if len(log) != 4 {
		t.Fatalf("log %v", log)
	}
}

func TestChooseIdeaRejectsUnknownAndBadScores(t *testing.T) {
	c := config.Select{MinFree: 3, MinLight: 3}
	if _, _, err := ChooseIdea([]SelectScore{sc("z", 4, 4, 4, 4, 4)}, map[string]bool{"a": true}, c); err == nil {
		t.Fatal("候補にないものを通した")
	}
	if _, _, err := ChooseIdea([]SelectScore{sc("a", 6, 4, 4, 4, 4)}, map[string]bool{"a": true}, c); err == nil {
		t.Fatal("6 点を通した")
	}
	if _, _, err := ChooseIdea([]SelectScore{sc("a", 4, 1, 4, 4, 4)}, map[string]bool{"a": true}, c); err == nil {
		t.Fatal("全部足切りでもエラーにならない")
	}
}

func TestDecide(t *testing.T) {
	c := config.Evaluate{ShipMinWakuwaku: 4, ShipMinQuality: 3}
	e := func(core bool, w, q int) EvalScore {
		return EvalScore{CoreWorks: core, Wakuwaku: w, Quality: q, Summary: "s", Weakest: "w"}
	}
	cases := []struct {
		e    EvalScore
		iter int
		want string
	}{
		{e(true, 4, 3), 0, "ship"},
		{e(true, 3, 5), 0, "iterate"},
		{e(true, 3, 5), 1, "iterate"},
		{e(true, 3, 5), 2, "drop"},
		{e(false, 5, 5), 0, "drop"},
	}
	for _, c2 := range cases {
		got, _, err := Decide(c2.e, c2.iter, 2, c)
		if err != nil || got != c2.want {
			t.Errorf("%+v iter=%d: got %s %v, want %s", c2.e, c2.iter, got, err, c2.want)
		}
	}
	if _, _, err := Decide(EvalScore{CoreWorks: true, Wakuwaku: 4, Quality: 4}, 0, 2, c); err == nil {
		t.Error("summary なしを通した")
	}
}

func TestDecideIterateMin(t *testing.T) {
	c := config.Evaluate{ShipMinWakuwaku: 4, ShipMinQuality: 3, IterateMinWakuwaku: 3}
	e := EvalScore{CoreWorks: true, Wakuwaku: 2, Quality: 4, Summary: "s", Weakest: "w"}
	if v, why, err := Decide(e, 0, 2, c); err != nil || v != "drop" || !strings.Contains(why, "わくわく 2 < 3") {
		t.Fatal(v, why, err)
	}
	e.Wakuwaku = 3
	if v, _, _ := Decide(e, 0, 2, c); v != "iterate" {
		t.Fatal(v)
	}
}

func TestDecideIterateMinEdges(t *testing.T) {
	c := config.Evaluate{ShipMinWakuwaku: 5, ShipMinQuality: 3, IterateMinWakuwaku: 3}
	base := EvalScore{CoreWorks: true, Wakuwaku: 3, Quality: 4, Summary: "s", Weakest: "w"}
	cases := []struct {
		name      string
		mut       func(*EvalScore)
		iteration int
		want, why string
	}{
		{"核が動かない (わくわくは高い)", func(e *EvalScore) { e.CoreWorks, e.Wakuwaku = false, 4 }, 0, "drop", "核が本番で動かない"},
		{"ちょうど下限", func(e *EvalScore) {}, 1, "iterate", "ブラッシュアップ 2/2"},
		{"下限の 1 つ下", func(e *EvalScore) { e.Wakuwaku = 2 }, 1, "drop", "わくわく 2 < 3"},
		{"回数の上限 (わくわくは足りる)", func(e *EvalScore) {}, 2, "drop", "ブラッシュアップ 2 回でも"},
		{"回数の上限でわくわくも足りない", func(e *EvalScore) { e.Wakuwaku = 2 }, 2, "drop", "ブラッシュアップ 2 回でも"},
	}
	for _, k := range cases {
		e := base
		k.mut(&e)
		v, why, err := Decide(e, k.iteration, 2, c)
		if err != nil || v != k.want || !strings.Contains(why, k.why) {
			t.Fatal(k.name, v, why, err)
		}
	}
}
