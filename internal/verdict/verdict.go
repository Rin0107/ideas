// Package verdict は LLM が付けた点数から、選ぶアイディア・評価の結論を決める。
// 点数は LLM、結論はルール。LLM に「ship していいか」を決めさせない。
package verdict

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Rin0107/ideas/internal/config"
)

// SelectScore は select で 1 件のアイディアに付ける点 (各 1〜5)。harness/rubric.md を参照。
type SelectScore struct {
	Idea     string `json:"idea"`     // 00141-regex-crossword
	Wakuwaku int    `json:"wakuwaku"` // わくわく
	Light    int    `json:"light"`    // MVP の軽さ (30 tick で作れるか)
	Free     int    `json:"free"`     // 無料枠で成立するか
	Own      int    `json:"own"`      // 自分ごと (ユーザーの関心に近いか)
	Findy    int    `json:"findy"`    // Go/TS の公開実績として見せられるか
	Hardware bool   `json:"hardware"` // 物・現地・人手が要る (ソフトだけで完結しない)
	// Sensitive は、体験の核に重要な情報 (個人を特定できる情報・認証情報・支払い・位置の履歴・
	// 健康・他人の情報・未成年の情報) をサーバーに持つ必要があるか。true なら必ず除外 (セキュリティが最優先)
	Sensitive bool `json:"sensitive"`
	// NeedsServer は、体験の核にサーバーの処理か保存が要るか (静的ファイルとブラウザだけで成り立たない)。
	// 工場のアプリはサーバーを持たない (配信は静的ファイルだけ) ので、true なら必ず除外
	NeedsServer bool   `json:"needs_server"`
	DataPlan    string `json:"data_plan"` // ブラウザにだけ置くもの (1〜2 文)。何も持たないなら「なし」
	Stack       string `json:"stack"`     // web / go-cli
	Reason      string `json:"reason"`
}

func (s SelectScore) Total() int { return s.Wakuwaku + s.Light + s.Free + s.Own + s.Findy }

var stacks = map[string]bool{"web": true, "go-cli": true}

func (s SelectScore) validate() error {
	for name, v := range map[string]int{"wakuwaku": s.Wakuwaku, "light": s.Light, "free": s.Free, "own": s.Own, "findy": s.Findy} {
		if v < 1 || v > 5 {
			return fmt.Errorf("%s: %s=%d は 1〜5 でない", s.Idea, name, v)
		}
	}
	if s.Idea == "" {
		return errors.New("idea が空")
	}
	if !stacks[s.Stack] {
		return fmt.Errorf("%s: stack %q は web / go-cli のどちらか", s.Idea, s.Stack)
	}
	if strings.TrimSpace(s.Reason) == "" {
		return fmt.Errorf("%s: reason が空", s.Idea)
	}
	if strings.TrimSpace(s.DataPlan) == "" {
		return fmt.Errorf("%s: data_plan が空 (何を持つかを必ず書く)", s.Idea)
	}
	return nil
}

// ChooseIdea は足切りを通ったものから合計点が最高のものを選ぶ。同点ならわくわく、次に ID の小さい方。
// allowed は候補として出したアイディア (それ以外の点数は不正として弾く)。
func ChooseIdea(scores []SelectScore, allowed map[string]bool, c config.Select) (SelectScore, []string, error) {
	var ok []SelectScore
	var log []string
	for _, s := range scores {
		if err := s.validate(); err != nil {
			return SelectScore{}, nil, err
		}
		if !allowed[s.Idea] {
			return SelectScore{}, nil, fmt.Errorf("%s は候補にない", s.Idea)
		}
		switch {
		case s.Sensitive:
			log = append(log, s.Idea+": 重要な情報を持つ必要があるので除外")
		case s.NeedsServer:
			log = append(log, s.Idea+": サーバーが要るので除外 (アプリは静的ファイルだけ)")
		case s.Hardware:
			log = append(log, s.Idea+": 物・現地が要るので除外")
		case s.Free < c.MinFree:
			log = append(log, fmt.Sprintf("%s: 無料 %d < %d で除外", s.Idea, s.Free, c.MinFree))
		case s.Light < c.MinLight:
			log = append(log, fmt.Sprintf("%s: 軽さ %d < %d で除外", s.Idea, s.Light, c.MinLight))
		default:
			ok = append(ok, s)
		}
	}
	if len(ok) == 0 {
		return SelectScore{}, log, errors.New("足切りを通ったアイディアがない")
	}
	sort.SliceStable(ok, func(i, j int) bool {
		a, b := ok[i], ok[j]
		if a.Total() != b.Total() {
			return a.Total() > b.Total()
		}
		if a.Wakuwaku != b.Wakuwaku {
			return a.Wakuwaku > b.Wakuwaku
		}
		return a.Idea < b.Idea
	})
	return ok[0], log, nil
}

// EvalScore は evaluate で、別の Claude が本番を触って付ける点。
type EvalScore struct {
	CoreWorks bool     `json:"core_works"` // 体験の核が本番で動くか
	Wakuwaku  int      `json:"wakuwaku"`   // 1〜5
	Quality   int      `json:"quality"`    // 1〜5 (見た目・使いやすさ・壊れにくさ)
	Bugs      []string `json:"bugs"`
	Weakest   string   `json:"weakest"` // わくわくを一番下げている点 (iterate の材料)
	Summary   string   `json:"summary"`
}

func (e EvalScore) validate() error {
	if e.Wakuwaku < 1 || e.Wakuwaku > 5 || e.Quality < 1 || e.Quality > 5 {
		return fmt.Errorf("wakuwaku=%d quality=%d は 1〜5 でない", e.Wakuwaku, e.Quality)
	}
	if strings.TrimSpace(e.Summary) == "" || strings.TrimSpace(e.Weakest) == "" {
		return errors.New("summary と weakest は必須")
	}
	return nil
}

// Decide は ship / iterate / drop を返す。
func Decide(e EvalScore, iteration, maxIterations int, c config.Evaluate) (string, string, error) {
	if err := e.validate(); err != nil {
		return "", "", err
	}
	// switch の case の条件はミューテーションの道具がカバレッジの範囲外と見なすので、if で書く
	if e.CoreWorks && e.Wakuwaku >= c.ShipMinWakuwaku && e.Quality >= c.ShipMinQuality {
		return "ship", fmt.Sprintf("核が動く・わくわく %d・品質 %d", e.Wakuwaku, e.Quality), nil
	}
	if e.CoreWorks && iteration < maxIterations && e.Wakuwaku >= c.IterateMinWakuwaku {
		return "iterate", fmt.Sprintf("核は動く・わくわく %d・品質 %d (ブラッシュアップ %d/%d)", e.Wakuwaku, e.Quality, iteration+1, maxIterations), nil
	}
	if !e.CoreWorks {
		return "drop", "体験の核が本番で動かない", nil
	}
	if iteration < maxIterations {
		return "drop", fmt.Sprintf("磨いても伸びる見込みが薄い (わくわく %d < %d)", e.Wakuwaku, c.IterateMinWakuwaku), nil
	}
	return "drop", fmt.Sprintf("ブラッシュアップ %d 回でも基準に届かない (わくわく %d・品質 %d)", iteration, e.Wakuwaku, e.Quality), nil
}

// ReadJSON はファイル (- なら標準入力) の JSON を読む。
func ReadJSON(path string, v any) error {
	var b []byte
	var err error
	if path == "-" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
