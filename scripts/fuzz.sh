#!/bin/sh
# 全部の Fuzz ターゲットに、合わせて $1 秒を割り振って新しい入力を探す。見つかった失敗は testdata/fuzz に残る。
# 守りの部分 (wakuwaku guard で書き換え禁止)。
# 手元の機械を守る: 1 つの Fuzz に同時に動かす数は FUZZ_PARALLEL (既定 2、CI は 4)、メモリは 2G まで (scripts/limit.sh)
set -eu
budget="${1:-150}"
parallel="${FUZZ_PARALLEL:-$( [ -n "${CI-}" ] && echo 4 || echo 2 )}"
export GOMAXPROCS="$parallel"
lim="sh $(dirname "$0")/limit.sh"
list=$(for p in $(go list ${PKGS:-./...}); do
  for f in $($lim 2G "$parallel" go test -list '^Fuzz' "$p" | grep '^Fuzz' || true); do echo "$p $f"; done
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
  $lim 2G "$parallel" go test -parallel "$parallel" -run '^$' -fuzz "^${f}\$" -fuzztime "${per}s" "$p"
done
