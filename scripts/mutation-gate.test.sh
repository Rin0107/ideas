#!/bin/sh
# scripts/mutation-gate.sh の自己検査 (CI の test で毎回流す)。gremlins の本物の出力の形で、通る・落ちるを確かめる。
set -eu
dir=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -r "$tmp"' EXIT
n=0
check() { # check <want: ok|fail> <名前> <出力>
  n=$((n + 1))
  printf '%s\n' "$3" > "$tmp/out"
  if sh "$dir/mutation-gate.sh" "$tmp/out" > "$tmp/log" 2>&1; then got=ok; else got=fail; fi
  if [ "$got" != "$1" ]; then
    echo "NG: $2 → $got (欲しいのは $1)" >&2
    cat "$tmp/log" >&2
    exit 1
  fi
  echo "ok: $2 → $got"
}
report() { # report <killed> <lived> <not covered> <efficacy> <coverage>
  printf 'Mutation testing completed in 46 seconds\nKilled: %s, Lived: %s, Not covered: %s\nTimed out: 0, Not viable: 0, Skipped: 282\nTest efficacy: %s%%\nMutator coverage: %s%%\n' "$@"
}
check ok   "全部殺した"                     "$(report 37 0 0 100.00 100.00)"
check fail "2026-11-10 の CI: 通らない変異 2 (98%)" "$(report 98 0 2 100.00 98.00)"
check fail "通らない変異 1"                  "$(report 500 0 1 100.00 99.80)"
check ok   "efficacy ちょうど 95%"           "$(report 19 1 0 95.00 100.00)"
check fail "efficacy 94.73%"               "$(report 18 1 0 94.73 100.00)"
check ok   "efficacy 95.00% 以上 (丸めない)"   "$(report 39 2 0 95.12 100.00)"
check fail "efficacy 94.99% (丸めると 95.0)" "$(report 1899 101 0 94.95 100.00)"
check fail "両方だめ"                        "$(report 1 1 1 50.00 66.67)"
check ok   "試す変異が 0"                    "$(report 0 0 0 0.00 0.00)"
check ok   "結果なし (gremlins の No results)" "$(printf '\nNo results to report.\n')"
check fail "結果の行がない (途中で止まった)"     "$(printf 'Gathering coverage... done\npanic: x\n')"
check fail "空"                            ""
check ok   "色つきの出力"                     "$(printf '\033[32mKilled: 3\033[0m, \033[31mLived: 0\033[0m, Not covered: 0\n')"
check fail "最後の結果の行で判定する"            "$(report 9 0 0 100 100; report 9 0 1 100 90)"
if sh "$dir/mutation-gate.sh" "$tmp/none" > /dev/null 2>&1; then echo "NG: 出力のファイルがないのに通った" >&2; exit 1; fi
echo "ok: mutation-gate の自己検査 $n 件"
