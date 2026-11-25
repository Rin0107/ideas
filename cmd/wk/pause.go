package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Rin0107/ideas/internal/state"
)

// ---- pause / resume ----
// 持ち主がアプリをいったん休ませる (2026-11-25、kisekae-closet を止めてほかのアイディアへ)。
// drop と違い、本番からも消さず repo も archive しない。休んだアプリは tick・計画・作りかけの数 (max_wip) に入らない。
// 主力 (focus) のアプリを休ませるときは focus も外す (残すと、主力がないとみて同じアイディアを選び直す)。

func cmdPause(args []string) error {
	fs := flag.NewFlagSet("pause", flag.ContinueOnError)
	repo := fs.String("repo", "", "休ませるアプリの repo (owner/name)")
	reason := fs.String("reason", "", "休ませる理由")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len([]rune(strings.TrimSpace(*reason))) < 10 {
		return errors.New("--reason を具体的に書く (10 文字以上)")
	}
	e, err := load()
	if err != nil {
		return err
	}
	p := e.pf.ByRepo(*repo)
	if p != nil && p.Status == state.Paused {
		fmt.Printf("%s はもう休んでいる\n", p.Repo)
		return nil
	}
	if p == nil || !p.Active() {
		return fmt.Errorf("作りかけのアプリに %q がない", *repo)
	}
	if e.cfg.Focus == p.Idea {
		if err := clearFocus(e.cfg.Root, p.Idea); err != nil {
			return err
		}
	}
	p.PausedFrom, p.Status = p.Status, state.Paused
	e.event(p, "paused", *reason)
	return e.save()
}

func cmdResume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	repo := fs.String("repo", "", "戻すアプリの repo (owner/name)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := load()
	if err != nil {
		return err
	}
	p := e.pf.ByRepo(*repo)
	if p == nil || p.Status != state.Paused {
		return fmt.Errorf("休んでいるアプリに %q がない", *repo)
	}
	if n := len(e.pf.Actives()); n >= e.cfg.WIP() {
		return fmt.Errorf("作りかけがもう %d 件ある (max_wip %d)。どれかを休ませるか出してから戻す", n, e.cfg.WIP())
	}
	p.Status, p.PausedFrom = p.PausedFrom, ""
	e.event(p, "resumed", "持ち主が戻した")
	return e.save()
}

// clearFocus は factory.json の focus を空にする (ほかの設定の書き方はそのまま)。
func clearFocus(root, idea string) error {
	path := filepath.Join(root, "factory.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`"focus"\s*:\s*"` + regexp.QuoteMeta(idea) + `"`)
	if !re.Match(b) {
		return fmt.Errorf("factory.json の focus を書きかえられない (%s)", idea)
	}
	return os.WriteFile(path, re.ReplaceAll(b, []byte(`"focus": ""`)), 0o644)
}
