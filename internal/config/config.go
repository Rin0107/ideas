// Package config は factory.json (予算・上限・守りの設定) を読む。
// factory.json はユーザーだけが変える (guard-edit で守られている)。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Rin0107/ideas/internal/limits"
)

type Select struct {
	Candidates int `json:"candidates"`
	MinFree    int `json:"min_free"`
	MinLight   int `json:"min_light"`
}

type Evaluate struct {
	ShipMinWakuwaku int `json:"ship_min_wakuwaku"`
	ShipMinQuality  int `json:"ship_min_quality"`
	// IterateMinWakuwaku は磨く価値があるとみなすわくわくの下限 (v3)。届かなければ磨かずに捨てる。0 なら下限なし
	IterateMinWakuwaku int `json:"iterate_min_wakuwaku"`
}

// Limits: File は scripts/claude-statusline.sh が書く利用上限の記録。空なら使わない (1 日の予算だけで止める)。
// FiveHourMax・SevenDayMax は工場が使ってよい割合 (%)。MaxAgeMinutes より古い記録は使わない。0 は既定 (70・85・180)
type Limits struct {
	File          string  `json:"file"`
	FiveHourMax   float64 `json:"five_hour_max_pct"`
	SevenDayMax   float64 `json:"seven_day_max_pct"`
	MaxAgeMinutes int     `json:"max_age_minutes"`
}

// Policy は Limits の既定を埋めた、工場が使ってよい範囲。
func (l Limits) Policy() limits.Policy {
	p := limits.Policy{FiveHourMax: 70, SevenDayMax: 85, MaxAge: 3 * time.Hour}
	if l.FiveHourMax > 0 {
		p.FiveHourMax = l.FiveHourMax
	}
	if l.SevenDayMax > 0 {
		p.SevenDayMax = l.SevenDayMax
	}
	if l.MaxAgeMinutes > 0 {
		p.MaxAge = time.Duration(l.MaxAgeMinutes) * time.Minute
	}
	return p
}

// Site: Repo は配信用の repo (owner/name)。URL は Cloudflare に連携したあとの本番の URL (https://wakuwaku-site.<sub>.workers.dev)。
// URL が空のあいだは Web アプリを公開できない。
type Site struct {
	Repo string `json:"repo"`
	URL  string `json:"url"`
}

// WebDB は web-db (Cloudflare Worker + D1、主力のアプリ) の配信 (docs/DESIGN-v3.md §10・docs/SETUP.md)。
// Subdomain は workers.dev のサブドメイン。D1 は repo (owner/name) → D1 の database_id (人が Cloudflare で作る。秘密ではない)。
type WebDB struct {
	Subdomain string            `json:"subdomain"`
	D1        map[string]string `json:"d1"`
}

// URL は repo の Worker の URL (末尾は /)。サブドメインが空なら空。
func (w WebDB) URL(repo string) string {
	if w.Subdomain == "" {
		return ""
	}
	return "https://" + filepath.Base(repo) + "." + w.Subdomain + ".workers.dev/"
}

type Config struct {
	Owner        string `json:"owner"`
	MainCheckout string `json:"main_checkout"`
	AppsDir      string `json:"apps_dir"`
	RuntimeDir   string `json:"runtime_dir"`
	IdeasDir     string `json:"ideas_dir"`

	TicksPerDay            int `json:"ticks_per_day"`
	MaxTicksMVP            int `json:"max_ticks_mvp"`
	MaxTicksIteration      int `json:"max_ticks_iteration"`
	MaxIterations          int `json:"max_iterations"`
	MaxAttemptsPerIssue    int `json:"max_attempts_per_issue"`
	MaxOpenIssues          int `json:"max_open_issues"`
	LockTTLMinutes         int `json:"lock_ttl_minutes"`
	MaxConsecutiveFailures int `json:"max_consecutive_failures"`

	IntakeEveryDays int `json:"intake_every_days"`
	RetroEveryDays  int `json:"retro_every_days"`
	MinUntriedIdeas int `json:"min_untried_ideas"`

	// ロールのループ (docs/ROLES.md)
	StepsPerRun     int `json:"steps_per_run"`     // 1 回の実行で leader が進めてよい段の数 (0 以下は上限なし。1 日の上限は効く)
	PatrolEveryDays int `json:"patrol_every_days"` // ロールごとの見回りの間隔 (0 以下は見回りをしない)
	PatrolMinTicks  int `json:"patrol_min_ticks"`  // アプリにこの tick 数を使うまでは見回らない (見るものがまだない)
	MaxProposals    int `json:"max_proposals"`     // 1 回の見回りで出せる提案の数

	Select   Select   `json:"select"`
	Evaluate Evaluate `json:"evaluate"`

	AllowedServices []string `json:"allowed_services"`
	DenyPackages    []string `json:"deny_packages"`
	ProtectedPaths  []string `json:"protected_paths"`
	// ReviewablePaths は守りの部分のうち、守りのレビュー (harness/reviewers/guardian.md) を通せば工場が変えてよいもの。
	// 残り (制御のコード・設定・フック・レビューの指示・工場の CI) は工場の力を広げるので、人だけが変える (docs/DESIGN.md §9)
	ReviewablePaths []string `json:"reviewable_paths"`

	// v3 (docs/DESIGN-v3.md)
	// StateCheckout は wk だけが書く工場の clone。状態と記録はここに書き、コマンドの終わりに commit して push する。
	// 空なら今の worktree に書く (テストと v2 の形)
	StateCheckout string `json:"state_checkout"`
	// RunHours は tick を始めてよい時 (0〜23、手元の時刻)。空ならいつでも
	RunHours []int `json:"run_hours"`
	// MaxTokensPerDay は 1 日に使ってよい量 (入力 + キャッシュ作成 + 出力 + キャッシュの読み出しの 1/10。usage.Weighted)。0 なら上限なし
	MaxTokensPerDay int `json:"max_tokens_per_day"`
	// Limits は Claude の利用上限 (5 時間・週) で止める (docs/DESIGN-v3.md §9)。記録が新しければ max_tokens_per_day の代わりに使う
	Limits Limits `json:"limits"`
	// Reviewers は PR に approve が要るロール。空ならファイルから決める (sre・engineer・画面なら designer)
	Reviewers []string `json:"reviewers"`
	// MaxMVPIssues は最初の版 (試作) の issue の上限。0 なら max_open_issues
	MaxMVPIssues int `json:"max_mvp_issues"`

	// 同時に進める (v3.1、docs/DESIGN-v3.md §8)
	// MaxParallel は同時に動いてよい実行の数。MaxWIP は同時に持ってよい作りかけのアプリの数。0 以下は 1
	MaxParallel int `json:"max_parallel"`
	MaxWIP      int `json:"max_wip"`

	// Focus は主力のアプリのアイディア (v3.3、docs/DESIGN-v3.md §10)。空でなければ工場はそれだけを進め、捨てず・出して終わりにせず磨き続ける
	Focus string `json:"focus"`

	// Site は公開した Web アプリをまとめて配る repo (Cloudflare の GitHub 連携で配信する)
	Site Site `json:"site"`
	// WebDB は web-db のアプリ (Worker + D1) の配信
	WebDB WebDB `json:"web_db"`

	// V4 は計画 (Opus) と実行 (Sonnet) を分ける形 (docs/DESIGN-v4.md)
	V4 V4 `json:"v4"`

	// Root は factory.json があるディレクトリ (今の worktree)。読み込み時に入る
	Root string `json:"-"`
}

// Load は dir から上にさかのぼって factory.json を探して読む。
func Load(dir string) (*Config, error) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for {
		p := filepath.Join(d, "factory.json")
		if b, err := os.ReadFile(p); err == nil {
			var c Config
			if err := json.Unmarshal(b, &c); err != nil {
				return nil, fmt.Errorf("%s: %w", p, err)
			}
			c.Root = d
			c.MainCheckout = Expand(c.MainCheckout)
			c.AppsDir = Expand(c.AppsDir)
			c.RuntimeDir = Expand(c.RuntimeDir)
			c.StateCheckout = Expand(c.StateCheckout)
			c.Limits.File = Expand(c.Limits.File)
			return &c, c.validate()
		}
		parent := filepath.Dir(d)
		if parent == d {
			return nil, fmt.Errorf("factory.json が見つからない (%s から上)", dir)
		}
		d = parent
	}
}

func (c *Config) validate() error {
	var bad []string
	for name, v := range map[string]int{
		"ticks_per_day": c.TicksPerDay, "max_ticks_mvp": c.MaxTicksMVP,
		"max_ticks_iteration": c.MaxTicksIteration, "max_attempts_per_issue": c.MaxAttemptsPerIssue,
		"lock_ttl_minutes": c.LockTTLMinutes, "max_consecutive_failures": c.MaxConsecutiveFailures,
	} {
		if v <= 0 {
			bad = append(bad, name)
		}
	}
	if c.Owner == "" {
		bad = append(bad, "owner")
	}
	if len(bad) > 0 {
		return fmt.Errorf("factory.json: 正の値が必要: %s", strings.Join(bad, ", "))
	}
	return nil
}

// Expand は先頭の ~ をホームディレクトリにする。
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// HardProtected は守りの部分のうち、工場が変えられないもの (protected_paths から reviewable_paths を除いたもの)。
func (c *Config) HardProtected() []string {
	var out []string
	for _, p := range c.ProtectedPaths {
		if !slices.Contains(c.ReviewablePaths, p) {
			out = append(out, p)
		}
	}
	return out
}

// Parallel は同時に動いてよい実行の数 (1 以上)。
func (c *Config) Parallel() int { return max(c.MaxParallel, 1) }

// WIP は同時に持ってよい作りかけのアプリの数 (1 以上)。
func (c *Config) WIP() int { return max(c.MaxWIP, 1) }

// V4 は計画と実行を分ける形 (docs/DESIGN-v4.md)。毎日 1 回 Opus が計画 (task の issue と完了条件のテスト) を作り、
// tick の Sonnet は task を 1 件ずつ、wk verify が通るまでやる。Enabled でなければ v3 のまま
type V4 struct {
	Enabled bool `json:"enabled"`
	// MinTasks は 1 回の計画で作る task の下限 (1 晩の tick が空回りしないように)。0 は既定 (20)
	MinTasks int `json:"min_tasks"`
	// TaskAttempts は task 1 件の試行の上限。届いたら wk:blocked (次の計画が分け直す)。0 は既定 (2)
	TaskAttempts int `json:"task_attempts"`
	// CheckTimeoutSeconds は完了条件のコマンド 1 つの時間切れ。0 は既定 (600)
	CheckTimeoutSeconds int `json:"check_timeout_seconds"`
}

func (v V4) Min() int      { return defaulted(v.MinTasks, 20) }
func (v V4) Attempts() int { return defaulted(v.TaskAttempts, 2) }
func (v V4) CheckTimeout() time.Duration {
	return time.Duration(defaulted(v.CheckTimeoutSeconds, 600)) * time.Second
}

func defaulted(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}
