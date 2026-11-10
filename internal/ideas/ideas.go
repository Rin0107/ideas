// Package ideas は ideas_wakuwaku/ のアイディアを読む。
package ideas

import (
	"bufio"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// 雛形の見出し (この順で全部あること)
var RequiredSections = []string{"概要", "わくわくポイント", "シーン", "体験の核", "ターゲット", "マネタイズ", "差別化ポイント"}

var dirRe = regexp.MustCompile(`^(\d{5})-([a-z0-9]+(?:-[a-z0-9]+)*)$`)

type Idea struct {
	Dir      string // 00141-regex-crossword
	ID       string // 00141
	Slug     string // regex-crossword
	Title    string
	Sections map[string]string
	Never    bool // 「## 履歴」に「wk:never」がある = 二度と選ばない
}

// Load は dir の下のアイディアを ID 順に読む。雛形 (idea-template) は飛ばす。
func Load(dir string) ([]*Idea, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []*Idea
	for _, e := range ents {
		if !e.IsDir() || e.Name() == "idea-template" {
			continue
		}
		m := dirRe.FindStringSubmatch(e.Name())
		if m == nil {
			out = append(out, &Idea{Dir: e.Name()}) // 形の検査で弾く
			continue
		}
		idea, err := parse(filepath.Join(dir, e.Name(), "idea.md"))
		if err != nil {
			return nil, err
		}
		idea.Dir, idea.ID, idea.Slug = e.Name(), m[1], m[2]
		out = append(out, idea)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}

func parse(path string) (*Idea, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	idea := &Idea{Sections: map[string]string{}}
	var cur string
	var buf strings.Builder
	flush := func() {
		if cur != "" {
			idea.Sections[cur] = strings.TrimSpace(buf.String())
		}
		buf.Reset()
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "# ") && idea.Title == "":
			idea.Title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
		case strings.HasPrefix(line, "## "):
			flush()
			cur = strings.TrimSpace(strings.TrimPrefix(line, "## "))
		default:
			buf.WriteString(line + "\n")
		}
	}
	flush()
	idea.Never = strings.Contains(idea.Sections["履歴"], "wk:never")
	return idea, sc.Err()
}

// Problems はアイディアの形の問題を返す (wk check 用)。
func Problems(all []*Idea) []string {
	var errs []string
	titles := map[string]string{}
	slugs := map[string]string{}
	for _, i := range all {
		if i.ID == "" {
			errs = append(errs, fmt.Sprintf("%s: フォルダ名が <5桁ID>-<slug> でない", i.Dir))
			continue
		}
		if i.Title == "" {
			errs = append(errs, i.Dir+": タイトル (# 行) がない")
		}
		for _, s := range RequiredSections {
			if strings.TrimSpace(i.Sections[s]) == "" {
				errs = append(errs, fmt.Sprintf("%s: 「## %s」がない・空", i.Dir, s))
			}
		}
		if d, ok := slugs[i.Slug]; ok {
			errs = append(errs, fmt.Sprintf("%s: slug が %s と同じ", i.Dir, d))
		}
		slugs[i.Slug] = i.Dir
		key := normalize(i.Title)
		if d, ok := titles[key]; ok && key != "" {
			errs = append(errs, fmt.Sprintf("%s: タイトルが %s と同じ", i.Dir, d))
		}
		titles[key] = i.Dir
	}
	return errs
}

func normalize(title string) string {
	t, _, _ := strings.Cut(title, "—")
	return strings.ToLower(strings.Join(strings.Fields(t), ""))
}

// NextID は次の通し番号。
func NextID(all []*Idea) string {
	max := 0
	for _, i := range all {
		var n int
		if _, err := fmt.Sscanf(i.ID, "%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("%05d", max+1)
}

// Candidates は未着手のアイディアから n 件を、seed で決まる順に選ぶ (同じ日なら同じ結果)。
func Candidates(all []*Idea, tried map[string]bool, n int, seed string) []*Idea {
	var pool []*Idea
	for _, i := range all {
		if i.ID != "" && !tried[i.Dir] && !i.Never {
			pool = append(pool, i)
		}
	}
	h := fnv.New64a()
	h.Write([]byte(seed))
	r := rand.New(rand.NewPCG(h.Sum64(), 0x9e3779b97f4a7c15))
	r.Shuffle(len(pool), func(a, b int) { pool[a], pool[b] = pool[b], pool[a] })
	if len(pool) > n {
		pool = pool[:n]
	}
	return pool
}

// Untried は未着手の件数。
func Untried(all []*Idea, tried map[string]bool) int {
	n := 0
	for _, i := range all {
		if i.ID != "" && !tried[i.Dir] && !i.Never {
			n++
		}
	}
	return n
}

var indexLine = regexp.MustCompile(`^- (\d{5}) ([a-z0-9-]+) — `)

// IndexProblems は INDEX.md (1 件 1 行の索引) とフォルダの食い違いを返す。
func IndexProblems(index string, all []*Idea) []string {
	got := map[string]string{}
	for _, l := range strings.Split(index, "\n") {
		if m := indexLine.FindStringSubmatch(l); m != nil {
			got[m[1]] = m[2]
		}
	}
	var errs []string
	for _, i := range all {
		switch s, ok := got[i.ID]; {
		case !ok:
			errs = append(errs, "INDEX.md に "+i.Dir+" の行がない")
		case s != i.Slug:
			errs = append(errs, fmt.Sprintf("INDEX.md の %s の slug が %s (フォルダは %s)", i.ID, s, i.Slug))
		}
		delete(got, i.ID)
	}
	for id, s := range got {
		errs = append(errs, "INDEX.md の "+id+" "+s+" のフォルダがない")
	}
	sort.Strings(errs)
	return errs
}
