// Package config は factory.json (予算・上限・守りの設定) を読む。
// factory.json はユーザーだけが変える (guard-edit で守られている)。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Select struct {
	Candidates int `json:"candidates"`
	MinFree    int `json:"min_free"`
	MinLight   int `json:"min_light"`
}

type Evaluate struct {
	ShipMinWakuwaku int `json:"ship_min_wakuwaku"`
	ShipMinQuality  int `json:"ship_min_quality"`
}

// Site: Repo は配信用の repo (owner/name)。URL は Cloudflare に連携したあとの本番の URL (https://wakuwaku-site.<sub>.workers.dev)。
// URL が空のあいだは Web アプリを公開できない。
type Site struct {
	Repo string `json:"repo"`
	URL  string `json:"url"`
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

	// Site は公開した Web アプリをまとめて配る repo (Cloudflare の GitHub 連携で配信する)
	Site Site `json:"site"`

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
