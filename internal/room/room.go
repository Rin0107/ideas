// Package room は、重い仕事 (tick) を始める前に機械に余裕があるかを見る。
// 2026-11-10 にメモリが尽きて PC ごと落ちたことの再発防止。余裕がなければ始めない (止めるのは安全側)。
package room

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Limits は始めてよい条件。
type Limits struct {
	MinAvailMB  int // 使えるメモリ (MemAvailable) の下限
	MinCgroupMB int // 自分の cgroup の上限 (memory.high / memory.max、親もたどる) までの残りの下限
	MaxPSI      int // メモリ待ちの割合 (PSI some avg10、%) の上限
	MaxLoadPct  int // 1 分の load の、コア数に対する割合 (%) の上限
}

// Default は既定の条件 (nix-config の cgroup の上限 memory.high=5G より内側に収まるように)。
func Default() Limits {
	return Limits{MinAvailMB: 3072, MinCgroupMB: 2048, MaxPSI: 10, MaxLoadPct: 50}
}

// Problems は root (本番は "/") の下の /proc と /sys/fs/cgroup を読み、条件を満たさない理由を返す。空なら始めてよい。
// 読めないものは「余裕がない」として扱う (分からないときは止める)。ただし cgroup の上限がないことは問題にしない。
func Problems(root string, cores int, l Limits) []string {
	var out []string
	mem, err := os.ReadFile(filepath.Join(root, "proc/meminfo"))
	if err != nil {
		return []string{"メモリの量を読めない: " + err.Error()}
	}
	if avail, ok := memAvailableMB(string(mem)); !ok {
		out = append(out, "MemAvailable を読めない")
	} else if avail < l.MinAvailMB {
		out = append(out, fmt.Sprintf("メモリが足りない: 使える %dMB < %dMB", avail, l.MinAvailMB))
	}
	if psi, err := os.ReadFile(filepath.Join(root, "proc/pressure/memory")); err == nil {
		if p, ok := psiSome10(string(psi)); !ok {
			out = append(out, "PSI を読めない")
		} else if p > float64(l.MaxPSI) {
			out = append(out, fmt.Sprintf("メモリの取り合いが起きている: PSI some avg10=%.1f%% > %d%%", p, l.MaxPSI))
		}
	}
	if load, err := os.ReadFile(filepath.Join(root, "proc/loadavg")); err != nil {
		out = append(out, "load を読めない: "+err.Error())
	} else if f := strings.Fields(string(load)); len(f) == 0 {
		out = append(out, "load を読めない")
	} else if v, err := strconv.ParseFloat(f[0], 64); err != nil {
		out = append(out, "load を読めない: "+err.Error())
	} else if pct := v * 100 / float64(max(cores, 1)); pct > float64(l.MaxLoadPct) {
		out = append(out, fmt.Sprintf("CPU が混んでいる: load %.2f (%d コアの %.0f%%) > %d%%", v, cores, pct, l.MaxLoadPct))
	}
	if r, ok := cgroupRoomMB(root); ok && r < l.MinCgroupMB {
		out = append(out, fmt.Sprintf("cgroup の残りが少ない: %dMB < %dMB", r, l.MinCgroupMB))
	}
	return out
}

func memAvailableMB(meminfo string) (int, bool) {
	for line := range strings.SplitSeq(meminfo, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "MemAvailable:" {
			kb, err := strconv.Atoi(f[1])
			if err != nil || kb < 0 {
				return 0, false
			}
			return kb / 1024, true
		}
	}
	return 0, false
}

func psiSome10(psi string) (float64, bool) {
	for line := range strings.SplitSeq(psi, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "some" {
			continue
		}
		v, ok := strings.CutPrefix(f[1], "avg10=")
		if !ok {
			return 0, false
		}
		p, err := strconv.ParseFloat(v, 64)
		return p, err == nil && p >= 0
	}
	return 0, false
}

// cgroupRoomMB は自分の cgroup (v2) から根までたどり、上限までの残りの一番小さいものを返す。上限がなければ false。
func cgroupRoomMB(root string) (int, bool) {
	b, err := os.ReadFile(filepath.Join(root, "proc/self/cgroup"))
	if err != nil {
		return 0, false
	}
	rel := ""
	for line := range strings.SplitSeq(string(b), "\n") {
		if p, ok := strings.CutPrefix(line, "0::"); ok {
			rel = p
		}
	}
	best, found := 0, false
	for dir := filepath.Clean("/" + rel); dir != "/"; dir = filepath.Dir(dir) {
		d := filepath.Join(root, "sys/fs/cgroup", dir)
		cur, ok := readInt(filepath.Join(d, "memory.current"))
		if !ok {
			continue
		}
		for _, name := range []string{"memory.high", "memory.max"} {
			lim, ok := readInt(filepath.Join(d, name))
			if !ok {
				continue // "max" (上限なし) か読めない
			}
			if r := (lim - cur) / (1 << 20); !found || r < best {
				best, found = r, true
			}
		}
	}
	return best, found
}

func readInt(p string) (int, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return n, err == nil
}
