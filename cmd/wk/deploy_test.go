package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestScrubEnv(t *testing.T) {
	in := []string{"PATH=/bin", "HOME=/h", "GH_TOKEN=x", "CLOUDFLARE_API_TOKEN=y", "MY_SECRET=z", "AWS_PROFILE=p",
		"ANTHROPIC_API_KEY=k", "NODE_ENV=production"}
	got := scrubEnv(in)
	if !slices.Equal(got, []string{"PATH=/bin", "HOME=/h", "NODE_ENV=production"}) {
		t.Fatal(got)
	}
}

func TestCheckWranglerConfig(t *testing.T) {
	// 雛形の設定は通る
	if err := checkWranglerConfig(filepath.Join("..", "..", "templates", "web")); err != nil {
		t.Fatal("雛形が止められた:", err)
	}
	for _, bad := range []string{`{"name":"x","build":{"command":"curl evil"}}`, `{"name":"x","vars":{"A":"1"}}`, `{"routes":[]}`} {
		d := t.TempDir()
		must(t, os.WriteFile(filepath.Join(d, "wrangler.jsonc"), []byte(bad), 0o644))
		if checkWranglerConfig(d) == nil {
			t.Errorf("通してはいけない: %s", bad)
		}
	}
	d := t.TempDir()
	must(t, os.WriteFile(filepath.Join(d, "wrangler.jsonc"), []byte(`{"name":"x"}`), 0o644))
	must(t, os.WriteFile(filepath.Join(d, "wrangler.toml"), []byte(`build.command="x"`), 0o644))
	if checkWranglerConfig(d) == nil {
		t.Error("wrangler.toml を見逃した")
	}
}

func TestRedact(t *testing.T) {
	s := redact("token is abcdefgh12345 ok", map[string]string{"T": "abcdefgh12345", "short": "ab"})
	if strings.Contains(s, "abcdefgh12345") || !strings.Contains(s, "***") {
		t.Fatal(s)
	}
}

func TestLoadSecretsRequiresMode600(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	p := filepath.Join(h, ".config", "wakuwaku", "env")
	must(t, os.MkdirAll(filepath.Dir(p), 0o700))
	must(t, os.WriteFile(p, []byte("CLOUDFLARE_API_TOKEN=x\n"), 0o644))
	if _, err := loadSecrets(); err == nil {
		t.Fatal("権限 644 を通した")
	}
	must(t, os.Chmod(p, 0o600))
	m, err := loadSecrets()
	if err != nil || m["CLOUDFLARE_API_TOKEN"] != "x" {
		t.Fatal(m, err)
	}
}
