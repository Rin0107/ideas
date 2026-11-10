package room

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sys は偽の /proc と /sys/fs/cgroup を作る。値が "" のファイルは作らない。
type sys struct {
	meminfo, psi, loadavg, cgroup string
	cg                            map[string]string // "non-systemd/memory.current" → 値
}

func (s sys) root(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	put := func(p, v string) {
		if v == "" {
			return
		}
		p = filepath.Join(r, p)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("proc/meminfo", s.meminfo)
	put("proc/pressure/memory", s.psi)
	put("proc/loadavg", s.loadavg)
	put("proc/self/cgroup", s.cgroup)
	for k, v := range s.cg {
		put(filepath.Join("sys/fs/cgroup", k), v)
	}
	return r
}

const (
	mem8G   = "MemTotal:  8000000 kB\nMemFree: 1 kB\nMemAvailable:    8388608 kB\n" // 8192MB
	psiZero = "some avg10=0.00 avg60=0.00 avg300=0.00 total=0\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"
	loadLow = "0.50 0.40 0.30 1/100 1234\n"
	gib     = 1 << 30
)

func n(v int) string { return strconv.Itoa(v) }

func ok() sys {
	return sys{meminfo: mem8G, psi: psiZero, loadavg: loadLow, cgroup: "0::/non-systemd\n",
		cg: map[string]string{
			"non-systemd/memory.current": n(1 * gib),
			"non-systemd/memory.high":    n(5 * gib),
			"non-systemd/memory.max":     n(6 * gib),
		}}
}

// 既定は nix-config の配分 (WSL 6GB = Orca など 2G + テストなど 4G) に合わせる
func TestDefault(t *testing.T) {
	uid := strconv.Itoa(os.Getuid())
	want := Limits{MinAvailMB: 2048, MinCgroupMB: 512, MaxPSI: 10, MaxLoadPct: 50,
		Heavy: "user.slice/user-" + uid + ".slice/user@" + uid + ".service/heavy.slice", MinHeavyMB: 2048}
	if Default() != want {
		t.Fatal(Default())
	}
}

// テストでの条件 (境界値を分かりやすい数で)
var testLimits = Limits{MinAvailMB: 3072, MinCgroupMB: 2048, MaxPSI: 10, MaxLoadPct: 50, Heavy: "u/heavy", MinHeavyMB: 1024}

func TestProblems(t *testing.T) {
	l := testLimits
	cases := []struct {
		name string
		edit func(*sys)
		want string // "" なら問題なし
	}{
		{"余裕がある", func(*sys) {}, ""},
		{"cgroup の上限なし", func(s *sys) {
			s.cg["non-systemd/memory.high"], s.cg["non-systemd/memory.max"] = "max", "max\n"
		}, ""},
		{"cgroup の情報なし", func(s *sys) { s.cgroup = "" }, ""},
		{"cgroup v1 だけ", func(s *sys) { s.cgroup = "1:name=systemd:/x\n" }, ""},
		{"memory.current がない", func(s *sys) { delete(s.cg, "non-systemd/memory.current") }, ""},
		{"PSI がない (古いカーネル)", func(s *sys) { s.psi = "" }, ""},
		// 境界値: 使えるメモリ 3072MB はよく、3071MB はだめ
		{"メモリ ちょうど", func(s *sys) { s.meminfo = "MemAvailable: " + n(3072*1024) + " kB\n" }, ""},
		{"メモリ 1 つ下", func(s *sys) { s.meminfo = "MemAvailable: " + n(3072*1024-1) + " kB\n" }, "メモリが足りない: 使える 3071MB < 3072MB"},
		{"MemAvailable がない", func(s *sys) { s.meminfo = "MemTotal: 1 kB\n" }, "MemAvailable を読めない"},
		{"MemAvailable が数でない", func(s *sys) { s.meminfo = "MemAvailable: x kB\n" }, "MemAvailable を読めない"},
		{"MemAvailable が負", func(s *sys) { s.meminfo = "MemAvailable: -1 kB\n" }, "MemAvailable を読めない"},
		// 境界値: PSI 10% はよく、10.01% はだめ
		{"PSI ちょうど", func(s *sys) { s.psi = "some avg10=10.00 avg60=0 avg300=0 total=0\n" }, ""},
		{"PSI 少し上", func(s *sys) { s.psi = "some avg10=10.01 avg60=0 avg300=0 total=0\n" }, "メモリの取り合いが起きている: PSI some avg10=10.0% > 10%"},
		{"PSI に some がない", func(s *sys) { s.psi = "full avg10=0\n" }, "PSI を読めない"},
		{"PSI の形が違う", func(s *sys) { s.psi = "some avg60=0\n" }, "PSI を読めない"},
		{"PSI が数でない", func(s *sys) { s.psi = "some avg10=x\n" }, "PSI を読めない"},
		{"PSI が負", func(s *sys) { s.psi = "some avg10=-1\n" }, "PSI を読めない"},
		{"PSI の行が短い", func(s *sys) { s.psi = "some\nsome avg10=1\n" }, ""},
		// 境界値: 4 コアで load 2.00 (50%) はよく、2.04 (51%) はだめ
		{"load ちょうど", func(s *sys) { s.loadavg = "2.00 0 0 1/1 1\n" }, ""},
		{"load 少し上", func(s *sys) { s.loadavg = "2.04 0 0 1/1 1\n" }, "CPU が混んでいる: load 2.04 (4 コアの 51%) > 50%"},
		{"load がない", func(s *sys) { s.loadavg = "" }, "load を読めない: "},
		{"load が空", func(s *sys) { s.loadavg = " \n" }, "load を読めない"},
		{"load が数でない", func(s *sys) { s.loadavg = "x 0 0\n" }, "load を読めない: "},
		// 境界値: cgroup の残り 2048MB はよく、2047MB はだめ。親のほうがきつければ親で決まる
		{"cgroup ちょうど", func(s *sys) { s.cg["non-systemd/memory.current"] = n(5*gib - 2048<<20) }, ""},
		{"cgroup 1 つ下", func(s *sys) { s.cg["non-systemd/memory.current"] = n(5*gib - 2047<<20) }, "cgroup の残りが少ない: 2047MB < 2048MB"},
		{"memory.max のほうがきつい", func(s *sys) { s.cg["non-systemd/memory.max"] = n(2 * gib) }, "cgroup の残りが少ない: 1024MB < 2048MB"},
		{"親がきつい", func(s *sys) {
			s.cgroup = "0::/a/b\n"
			s.cg["a/b/memory.current"], s.cg["a/b/memory.max"] = n(1*gib), "max"
			s.cg["a/memory.current"], s.cg["a/memory.high"] = n(3*gib), n(4*gib)
		}, "cgroup の残りが少ない: 1024MB < 2048MB"},
		{"上限が数でない", func(s *sys) { s.cg["non-systemd/memory.high"], s.cg["non-systemd/memory.max"] = "x", "x" }, ""},
		// テストなどの枠: 残り 1024MB はよく、1023MB はだめ。枠がまだなければ親 (u) を見る
		{"枠 ちょうど", func(s *sys) {
			s.cg["u/heavy/memory.current"], s.cg["u/heavy/memory.max"] = n(3*gib), n(4*gib)
		}, ""},
		{"枠 1 つ下", func(s *sys) {
			s.cg["u/heavy/memory.current"], s.cg["u/heavy/memory.max"] = n(3*gib+1<<20), n(4*gib)
		}, "テストなどの枠の残りが少ない: 1023MB < 1024MB"},
		{"枠がまだない (親で見る)", func(s *sys) {
			s.cg["u/memory.current"], s.cg["u/memory.high"] = n(3*gib+512<<20), n(4*gib)
		}, "テストなどの枠の残りが少ない: 512MB < 1024MB"},
		{"枠にも親にも上限なし", func(s *sys) { s.cg["u/memory.current"] = n(3 * gib) }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := ok()
			c.edit(&s)
			got := Problems(s.root(t), 4, l)
			if c.want == "" {
				if len(got) != 0 {
					t.Fatal(got)
				}
				return
			}
			if len(got) != 1 || !strings.HasPrefix(got[0], c.want) {
				t.Fatalf("%q が欲しいが %q", c.want, got)
			}
		})
	}
}

func TestProblemsNoMeminfo(t *testing.T) {
	s := ok()
	s.meminfo = ""
	got := Problems(s.root(t), 4, Default())
	if len(got) != 1 || !strings.HasPrefix(got[0], "メモリの量を読めない") {
		t.Fatal(got)
	}
}

// コア数 0 (取れなかった) でも 0 で割らない
func TestProblemsZeroCores(t *testing.T) {
	s := ok()
	s.loadavg = "0.40 0 0\n"
	if got := Problems(s.root(t), 0, Default()); len(got) != 0 {
		t.Fatal(got)
	}
	s.loadavg = "0.51 0 0\n"
	if got := Problems(s.root(t), 0, Default()); len(got) != 1 {
		t.Fatal(got)
	}
}

// 全部がだめなら全部の理由を並べる
func TestProblemsAll(t *testing.T) {
	s := ok()
	s.meminfo = "MemAvailable: 1024 kB\n"
	s.psi = "some avg10=99 avg60=0\n"
	s.loadavg = "64 0 0\n"
	s.cg["non-systemd/memory.current"] = n(5 * gib)
	if got := Problems(s.root(t), 4, Default()); len(got) != 4 {
		t.Fatal(got)
	}
}

// 本物の機械でも落ちずに読める (値は機械による)
func TestProblemsReal(t *testing.T) {
	_ = Problems("/", 4, Default())
}
