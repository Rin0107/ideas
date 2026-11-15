// Package limits は Claude の利用上限 (5 時間・週) を見て、工場が今 tick を始めてよいかを決める。
// 値は Claude Code の statusline が受け取る rate_limits を scripts/claude-statusline.sh がファイルに書き留めたもの
// (鍵を持たずに読める。docs/DESIGN-v3.md §9)。工場はあなたの分を残すように、上限の手前で止まる:
//   - 5 時間: 使った割合が FiveHourMax 以上なら止める
//   - 週: 週の経過に合わせた目安 (SevenDayMax × (経過の割合 + Lead)、SevenDayMax まで) 以上なら止める。
//     週の初めに使い切らず、週の終わりにも SevenDayMax より上は残す
package limits

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Week は週の窓の長さ。Lead は週の目安を経過より先に進めておく分 (週の初めでも少しは動ける)。
const (
	Week time.Duration = 604_800_000_000_000 // 7 日 (掛け算で書くと、定数の式をミューテーションの道具がテストの外と数える)
	Lead               = 0.15
)

// Window は 1 つの窓 (5 時間か週) の使った割合 (%) と、戻る時刻 (unix 秒)。
type Window struct {
	Used     float64 `json:"used_percentage"`
	ResetsAt int64   `json:"resets_at"`
}

// Reading は書き留めた 1 回分。At はその時刻 (unix 秒)。
type Reading struct {
	At         int64 `json:"at"`
	RateLimits struct {
		FiveHour *Window `json:"five_hour"`
		SevenDay *Window `json:"seven_day"`
	} `json:"rate_limits"`
}

// Policy は工場が使ってよい範囲。MaxAge より古い記録は使わない (呼び出し側が 1 日の予算に戻す)。
type Policy struct {
	FiveHourMax float64
	SevenDayMax float64
	MaxAge      time.Duration
}

// Read は path の記録を返す。なければ nil。
func Read(path string) (*Reading, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Reading
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &r, nil
}

// Fresh は記録が MaxAge 以内で、両方の窓があるか。
func (r *Reading) Fresh(now time.Time, maxAge time.Duration) bool {
	ok := r.RateLimits.FiveHour != nil && r.RateLimits.SevenDay != nil
	return ok && now.Sub(time.Unix(r.At, 0)) <= maxAge
}

// UsedAt は now の時点の使った割合。窓が戻る時刻を過ぎていれば 0。
func (w *Window) UsedAt(now time.Time) float64 {
	if now.Unix() >= w.ResetsAt {
		return 0
	}
	return w.Used
}

// Target は週の窓の、now の時点の目安 (%)。
func Target(w *Window, now time.Time, top float64) float64 {
	start := time.Unix(w.ResetsAt, 0).Add(-Week)
	elapsed := min(max(float64(now.Sub(start))/float64(Week), 0), 1)
	return top * min(elapsed+Lead, 1)
}

// Problem は今 tick を始めてはいけない理由を返す。空なら始めてよい。r は Fresh であること。
func Problem(r *Reading, now time.Time, p Policy) string {
	five, week := r.RateLimits.FiveHour, r.RateLimits.SevenDay
	if u := five.UsedAt(now); u >= p.FiveHourMax {
		return fmt.Sprintf("5 時間の利用上限を %.0f%% 使った (工場は %.0f%% まで、%s に戻る)", u, p.FiveHourMax, clock(five.ResetsAt))
	}
	if u, t := week.UsedAt(now), Target(week, now, p.SevenDayMax); u >= t {
		return fmt.Sprintf("週の利用上限を %.0f%% 使った (今の目安 %.0f%%、%s に戻る)", u, t, clock(week.ResetsAt))
	}
	return ""
}

// Summary は今の使い方を 1 行で返す (wk usage)。
func Summary(r *Reading, now time.Time, p Policy) string {
	five, week := r.RateLimits.FiveHour, r.RateLimits.SevenDay
	return fmt.Sprintf("5 時間 %.0f%% (工場は %.0f%% まで、%s に戻る)・週 %.0f%% (今の目安 %.0f%%、最後は %.0f%% まで、%s に戻る)",
		five.UsedAt(now), p.FiveHourMax, clock(five.ResetsAt),
		week.UsedAt(now), Target(week, now, p.SevenDayMax), p.SevenDayMax, clock(week.ResetsAt))
}

func clock(unix int64) string { return time.Unix(unix, 0).Format("01/02 15:04") }
