#!/bin/sh
# 全部の Fuzz ターゲットに、合わせて $1 秒を割り振って新しい入力を探す。見つかった失敗は testdata/fuzz に残る。
# 守りの部分 (wakuwaku guard で書き換え禁止)。
set -eu
budget="${1:-150}"
list=$(for p in $(go list ./...); do
  for f in $(go test -list '^Fuzz' "$p" | grep '^Fuzz' || true); do echo "$p $f"; done
done)
n=$(printf '%s\n' "$list" | grep -c . || true)
if [ "$n" -eq 0 ]; then
  echo "Fuzz のテストがない (入力を受け取る関数ごとに FuzzXxx を書く)" >&2
  exit 1
fi
per=$((budget / n))
[ "$per" -lt 10 ] && per=10
printf '%s\n' "$list" | while read -r p f; do
  echo "fuzz: $f ($p) ${per}s"
  go test -run '^$' -fuzz "^${f}\$" -fuzztime "${per}s" "$p"
done
