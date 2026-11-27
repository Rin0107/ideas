package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Rin0107/ideas/internal/config"
	"github.com/Rin0107/ideas/internal/state"
)

func TestPauseAndResume(t *testing.T) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"focus": "00001-alpha", "max_wip": 2})
	f.projects(active(func(p *state.Project) { p.Status = state.Iterating }),
		&state.Project{Idea: "00002-beta", Repo: "Tester/beta", Status: state.Building, Stack: "web", Started: "2026-11-02"})
	must(t, cmdPause([]string{"--repo", "Tester/alpha", "--reason", "いったん止めてほかのアイディアへ"}))
	p := f.portfolio().ByRepo("Tester/alpha")
	if p.Status != state.Paused || p.PausedFrom != state.Iterating || p.Active() {
		t.Fatalf("%+v", p)
	}
	if h := p.History[len(p.History)-1]; h.Event != "paused" || h.Note != "いったん止めてほかのアイディアへ" {
		t.Fatalf("%+v", h)
	}
	if cfg := f.read("factory.json"); !strings.Contains(cfg, `"focus": ""`) || strings.Contains(cfg, "00001-alpha") {
		t.Fatal(cfg)
	}
	// 2 回目は何もしない (予約が重なっても壊れない)
	out, err := capture(t, func() error {
		return cmdPause([]string{"--repo", "Tester/alpha", "--reason", "いったん止めてほかのアイディアへ"})
	})
	if err != nil || !strings.Contains(out, "もう休んでいる") || len(f.portfolio().ByRepo("Tester/alpha").History) != 1 {
		t.Fatal(out, err)
	}
	// 主力でないものは focus に触らない
	f.writeConfig(map[string]any{"focus": "", "max_wip": 2})
	must(t, cmdPause([]string{"--repo", "Tester/beta", "--reason", "こちらも少し休ませておく"}))
	if f.portfolio().ByRepo("Tester/beta").PausedFrom != state.Building {
		t.Fatal("前の状態が残っていない")
	}
	must(t, cmdResume([]string{"--repo", "Tester/alpha"}))
	p = f.portfolio().ByRepo("Tester/alpha")
	if p.Status != state.Iterating || p.PausedFrom != "" || p.History[len(p.History)-1].Event != "resumed" {
		t.Fatalf("%+v", p)
	}
}

// --focus は主力を付け替える (主力を休ませても、主力でないものを休ませても)
func TestPauseFocus(t *testing.T) {
	for name, from := range map[string]string{"主力を休ませる": "00001-alpha", "主力がない": ""} {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t)
			f.writeConfig(map[string]any{"focus": from})
			f.projects(active())
			must(t, cmdPause([]string{"--repo", "Tester/alpha", "--reason", "次の主力に場所をあける", "--focus", "00003-gamma"}))
			if cfg := f.read("factory.json"); !strings.Contains(cfg, `"focus": "00003-gamma"`) {
				t.Fatal(cfg)
			}
		})
	}
}

func TestPauseErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		setup func(f *factory)
		want  string
	}{
		"知らない引数":    {args: []string{"-x"}, want: "-x"},
		"理由が短い":     {args: []string{"--repo", "Tester/alpha", "--reason", "やめる"}, want: "10 文字"},
		"知らない repo": {args: []string{"--repo", "Tester/nope", "--reason", "いったん止めてほかのアイディアへ"}, want: `"Tester/nope" がない`},
		"終わったアプリ": {setup: func(f *factory) {
			f.projects(active(func(p *state.Project) { p.Status = state.Shipped }))
		}, want: "作りかけのアプリに"},
		"設定が壊れている": {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
		"次の主力がない": {args: []string{"--repo", "Tester/alpha", "--reason", "いったん止めてほかのアイディアへ", "--focus", "09999-nope"},
			want: `"09999-nope" がない`},
		"focus が読めない": {setup: func(f *factory) {
			f.writeConfig(map[string]any{"focus": "00001-alpha"})
			readOnly(t, filepath.Join(f.root, "factory.json"))
		}, want: "permission denied"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t)
			f.projects(active())
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"--repo", "Tester/alpha", "--reason", "いったん止めてほかのアイディアへ"}
			}
			errHas(t, cmdPause(args), c.want)
		})
	}
}

// 主力が複数: 休ませたものだけ外し、--focus は後ろに足す (もうあれば足さない)。書いたものは読み直せる
func TestPauseFocusList(t *testing.T) {
	f := newFactory(t)
	f.writeConfig(map[string]any{"focus": []string{"00001-alpha", "00002-beta"}})
	f.projects(active())
	must(t, cmdPause([]string{"--repo", "Tester/alpha", "--reason", "次の主力に場所をあける", "--focus", "00003-gamma"}))
	if cfg := f.read("factory.json"); !strings.Contains(cfg, `"focus": ["00002-beta","00003-gamma"]`) {
		t.Fatal(cfg)
	}
	if c := mustCfg(t, f.root); !slices.Equal(c.Focus, config.Focus{"00002-beta", "00003-gamma"}) {
		t.Fatal(c.Focus)
	}
	f.projects(active())
	must(t, cmdPause([]string{"--repo", "Tester/alpha", "--reason", "次の主力に場所をあける", "--focus", "00002-beta"}))
	if cfg := f.read("factory.json"); !strings.Contains(cfg, `"focus": ["00002-beta","00003-gamma"]`) {
		t.Fatal(cfg)
	}
}

func TestSetFocusErrors(t *testing.T) {
	errHas(t, setFocus(t.TempDir(), nil), "factory.json")
	f := newFactory(t)
	f.write("factory.json", "{}")
	errHas(t, setFocus(f.root, nil), "focus がない")
}

func TestResumeErrors(t *testing.T) {
	cases := map[string]struct {
		args  []string
		setup func(f *factory)
		want  string
	}{
		"知らない引数": {args: []string{"-x"}, want: "-x"},
		"休んでいない": {setup: func(f *factory) { f.projects(active()) }, want: "休んでいるアプリに"},
		"作りかけが多い": {setup: func(f *factory) {
			f.writeConfig(map[string]any{"max_wip": 1})
			f.projects(paused(), &state.Project{Idea: "00002-beta", Repo: "Tester/beta", Status: state.Building, Stack: "web", Started: "2026-11-02"})
		}, want: "max_wip 1"},
		"設定が壊れている": {setup: func(f *factory) { f.write("factory.json", "{") }, want: "factory.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFactory(t)
			f.projects(paused())
			if c.setup != nil {
				c.setup(f)
			}
			args := c.args
			if args == nil {
				args = []string{"--repo", "Tester/alpha"}
			}
			errHas(t, cmdResume(args), c.want)
		})
	}
}

func paused() *state.Project {
	return active(func(p *state.Project) { p.Status, p.PausedFrom = state.Paused, state.Iterating })
}
