package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestScrubEnv(t *testing.T) {
	in := []string{"PATH=/bin", "HOME=/h", "GH_TOKEN=x", "CLOUDFLARE_API_TOKEN=y", "MY_SECRET=z", "AWS_PROFILE=p",
		"ANTHROPIC_API_KEY=k", "NODE_ENV=production"}
	got := scrubEnv(in)
	if !slices.Equal(got, []string{"PATH=/bin", "HOME=/h", "NODE_ENV=production"}) {
		t.Fatal(got)
	}
}

func TestCopyDist(t *testing.T) {
	mk := func(files map[string]string) string {
		d := t.TempDir()
		for name, body := range files {
			p := filepath.Join(d, name)
			must(t, os.MkdirAll(filepath.Dir(p), 0o755))
			must(t, os.WriteFile(p, []byte(body), 0o644))
		}
		return d
	}
	ok := mk(map[string]string{"index.html": "<p>hi", "assets/a.js": "x"})
	dst := filepath.Join(t.TempDir(), "out")
	if err := copyDist(ok, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "assets", "a.js")); string(b) != "x" {
		t.Fatal("写せていない")
	}
	for name, files := range map[string]map[string]string{
		"index.html なし":   {"a.js": "x"},
		"_headers":        {"index.html": "", "_headers": "/* X: y"},
		"入れ子の _redirects": {"index.html": "", "a/_redirects": "/ https://evil 302"},
		"_worker.js":      {"index.html": "", "_worker.js": "export default {}"},
		"Service Worker":  {"index.html": "", "sw.js": ""},
	} {
		if copyDist(mk(files), filepath.Join(t.TempDir(), "out")) == nil {
			t.Errorf("%s を通した", name)
		}
	}
	link := mk(map[string]string{"index.html": ""})
	must(t, os.Symlink("/etc/passwd", filepath.Join(link, "p")))
	if copyDist(link, filepath.Join(t.TempDir(), "out")) == nil {
		t.Error("リンクを通した")
	}
}

func TestCheckRunsState(t *testing.T) {
	for _, c := range []struct {
		in       string
		done, ok bool
	}{
		{"", false, false},
		{"Workers Builds: wakuwaku-site\tin_progress\t", false, true},
		{"Workers Builds: wakuwaku-site\tcompleted\tsuccess\n", true, true},
		{"a\tcompleted\tsuccess\nb\tcompleted\tfailure", true, false},
	} {
		done, ok, _ := checkRunsState(c.in)
		if done != c.done || ok != c.ok {
			t.Errorf("%q: done=%v ok=%v", c.in, done, ok)
		}
	}
}

func TestPreviewCandidates(t *testing.T) {
	text := "Preview URL: https://0123abcd-wakuwaku-site.rin.workers.dev\n" +
		"evil: https://x-wakuwaku-site.attacker.workers.dev\nBranch: https://preview-wakuwaku-site.rin.workers.dev"
	got := previewCandidates("https://wakuwaku-site.rin.workers.dev", text)
	want := []string{"https://preview-wakuwaku-site.rin.workers.dev", "https://0123abcd-wakuwaku-site.rin.workers.dev"}
	if !slices.Equal(got, want) {
		t.Fatal(got)
	}
	if got := previewCandidates("https://wakuwaku-site.rin.workers.dev", ""); !slices.Equal(got, want[:1]) {
		t.Fatal(got)
	}
}

func TestRenderIndex(t *testing.T) {
	s := string(renderIndex([]string{"good-app", "<script>", "../x"}))
	if !strings.Contains(s, `href="/good-app/"`) || strings.Contains(s, "<script>") || strings.Contains(s, "../x") {
		t.Fatal(s)
	}
}

func TestSmoke(t *testing.T) {
	headers := map[string]string{
		"Content-Security-Policy": "default-src 'self'", "X-Content-Type-Options": "nosniff",
		"Referrer-Policy": "no-referrer", "Strict-Transport-Security": "max-age=1", "Permissions-Policy": "camera=()",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		if r.URL.Path == "/cookie" {
			w.Header().Set("Set-Cookie", "a=b")
		}
		w.Write([]byte("<p>v2\n"))
	}))
	defer srv.Close()
	if p := smoke(srv.URL+"/", []byte("<p>v2")); len(p) != 0 {
		t.Fatal(p)
	}
	if p := smoke(srv.URL+"/", []byte("<p>v1")); len(p) != 1 {
		t.Fatal("古い版を見逃した", p)
	}
	if p := smoke(srv.URL+"/cookie", nil); len(p) != 1 {
		t.Fatal("Cookie を見逃した", p)
	}
}

func TestLatestRun(t *testing.T) {
	since := mustTime("2026-11-10T03:00:00Z")
	out := `[{"createdAt":"2026-11-10T02:59:00Z","status":"completed","conclusion":"success","url":"old"},
	{"createdAt":"2026-11-10T03:01:00Z","status":"in_progress","conclusion":"","url":"a"},
	{"createdAt":"2026-11-10T03:02:00Z","status":"completed","conclusion":"failure","url":"b"}]`
	r, ok := latestRun(out, since)
	if !ok || r.URL != "b" {
		t.Fatal(r, ok)
	}
	if _, ok := latestRun(`[{"createdAt":"2026-11-10T02:00:00Z"}]`, since); ok {
		t.Fatal("古い実行を拾った")
	}
	if _, ok := latestRun("not json", since); ok {
		t.Fatal("壊れた出力を拾った")
	}
}

func mustTime(s string) time.Time {
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return tm
}
