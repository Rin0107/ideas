package limits

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)

func reading(at time.Time, five, week *Window) *Reading {
	r := &Reading{At: at.Unix()}
	r.RateLimits.FiveHour, r.RateLimits.SevenDay = five, week
	return r
}

// weekAt は now に、週の elapsed (0〜1) が経った週の窓。
func weekAt(used, elapsed float64) *Window {
	start := now.Add(-time.Duration(elapsed * float64(Week)))
	return &Window{Used: used, ResetsAt: start.Add(Week).Unix()}
}

var policy = Policy{FiveHourMax: 70, SevenDayMax: 80, MaxAge: time.Hour}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	if r, err := Read(filepath.Join(dir, "none")); r != nil || err != nil {
		t.Fatalf("ないとき = %v, %v", r, err)
	}
	p := filepath.Join(dir, "l.json")
	os.WriteFile(p, []byte(`{"at":100,"rate_limits":{"five_hour":{"used_percentage":12.5,"resets_at":200},"seven_day":{"used_percentage":40,"resets_at":300}}}`), 0o644)
	r, err := Read(p)
	if err != nil || r.At != 100 || r.RateLimits.FiveHour.Used != 12.5 || r.RateLimits.SevenDay.ResetsAt != 300 {
		t.Fatalf("Read = %+v, %v", r, err)
	}
	os.WriteFile(p, []byte(`{`), 0o644)
	if _, err := Read(p); err == nil || !strings.Contains(err.Error(), "l.json") {
		t.Fatalf("壊れた記録 = %v", err)
	}
	if _, err := Read(dir); err == nil {
		t.Fatal("ディレクトリは読めない")
	}
}

func TestFresh(t *testing.T) {
	w := &Window{}
	for _, c := range []struct {
		r    *Reading
		want bool
	}{
		{reading(now.Add(-time.Hour), w, w), true}, // ちょうど MaxAge
		{reading(now.Add(-time.Hour-time.Second), w, w), false},
		{reading(now, nil, w), false},
		{reading(now, w, nil), false},
	} {
		if got := c.r.Fresh(now, time.Hour); got != c.want {
			t.Errorf("Fresh(%+v) = %v", c.r, got)
		}
	}
}

func TestUsedAt(t *testing.T) {
	if u := (&Window{Used: 30, ResetsAt: now.Unix() + 1}).UsedAt(now); u != 30 {
		t.Errorf("戻る前 = %v", u)
	}
	if u := (&Window{Used: 30, ResetsAt: now.Unix()}).UsedAt(now); u != 0 {
		t.Errorf("戻った時刻ちょうど = %v", u)
	}
}

func TestTarget(t *testing.T) {
	for _, c := range []struct{ elapsed, want float64 }{
		{0, 12}, {0.5, 52}, {0.85, 80}, {1, 80},
		{-0.5, 12}, // 戻る時刻が 1 週より先 (時計のずれ)
		{2, 80},    // 戻る時刻を過ぎた
	} {
		if got := Target(weekAt(0, c.elapsed), now, 80); got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("Target(経過 %v) = %v, want %v", c.elapsed, got, c.want)
		}
	}
}

func TestProblem(t *testing.T) {
	five := func(u float64) *Window { return &Window{Used: u, ResetsAt: now.Add(time.Hour).Unix()} }
	for _, c := range []struct {
		name string
		r    *Reading
		want string
	}{
		{"余裕がある", reading(now, five(69), weekAt(51, 0.5)), ""},
		{"5 時間がちょうど上限", reading(now, five(70), weekAt(0, 0.5)), "5 時間の利用上限を 70% 使った (工場は 70% まで"},
		{"週が目安ちょうど", reading(now, five(0), weekAt(52, 0.5)), "週の利用上限を 52% 使った (今の目安 52%"},
		{"5 時間は戻った", reading(now, &Window{Used: 99, ResetsAt: now.Unix()}, weekAt(0, 0.5)), ""},
	} {
		got := Problem(c.r, now, policy)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: Problem = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSummary(t *testing.T) {
	r := reading(now, &Window{Used: 23, ResetsAt: now.Add(time.Hour).Unix()}, weekAt(41, 0.5))
	got := Summary(r, now, policy)
	want := "5 時間 23% (工場は 70% まで、" + now.Add(time.Hour).Format("01/02 15:04") + " に戻る)・週 41% (今の目安 52%、最後は 80% まで、"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("Summary = %q", got)
	}
}
