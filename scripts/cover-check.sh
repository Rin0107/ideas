#!/bin/sh
# カバレッジが 100% かを、丸めずに数える (go tool cover の 100.0% は 99.96% でも出るため)。
# 守りの部分 (wakuwaku guard で書き換え禁止)。
set -eu
prof="$1"
awk 'NR > 1 {
  split($0, a, " "); key = a[1]; stmts[key] = a[2]
  if (a[3] > 0) hit[key] = 1
}
END {
  miss = 0; total = 0
  for (k in stmts) { total += stmts[k]; if (!(k in hit)) { miss += stmts[k]; print "  通っていない: " k > "/dev/stderr" } }
  if (total == 0) { print "カバレッジの記録が空" > "/dev/stderr"; exit 1 }
  if (miss > 0) { printf "カバレッジ %.2f%% (%d/%d 文)。100%% が必要\n", 100 * (total - miss) / total, total - miss, total > "/dev/stderr"; exit 1 }
  printf "ok: カバレッジ 100%% (%d 文)\n", total
}' "$prof"
