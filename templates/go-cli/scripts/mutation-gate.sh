#!/bin/sh
# ミューテーションの関門: gremlins の出力を数えて、基準に届かなければ落とす。守りの部分 (wakuwaku guard で書き換え禁止)。
#
# なぜ gremlins の --threshold-efficacy / --threshold-mcover に任せないか (2026-11-11 に gremlins v0.6.0 と viper v1.21.0 のソースで確かめた):
#   - 2 つの引数は float の flag。viper は float の flag の値を文字列 ("100") で返す
#   - gremlins はそれを float でも int でも読めず 0 にし、0 は「基準なし」になる。つまり基準は一度も効いていなかった
#     (実際に CI で mutant coverage 98% が通った)
#   - しかも比べ方が `<=` なので、仮に読めても 100% ちょうどで落ちる
# そこで、出力の「Killed: K, Lived: L, Not covered: N」から、丸めずに整数で判定する:
#   - mutant coverage 100%: N = 0 (テストが通らない変異が 1 つもない)
#   - test efficacy 95% 以上: K * 100 >= 95 * (K + L)
#   - 試す変異が 0 (K + L + N = 0) は ok (変わった行に変異できる所がない)。道具の使い方の誤りは、雛形の CI の自己検査が見つける
# 使い方: sh scripts/mutation-gate.sh <gremlins の出力のファイル>   (基準は MUTATION_MIN_EFFICACY、既定 95)
set -eu
out="${1:?gremlins の出力のファイルを渡す}"
min="${MUTATION_MIN_EFFICACY:-95}"
# 色の付いた出力でも読めるように、端末の制御文字を外す
line=$(sed 's/\x1b\[[0-9;]*m//g' "$out" | grep -E 'Killed: [0-9]+, Lived: [0-9]+, Not covered: [0-9]+' | tail -1 || true)
if [ -z "$line" ]; then
  if grep -q 'No results to report' "$out"; then
    echo "ok: ミューテーション 試す変異がない"
    exit 0
  fi
  echo "ミューテーションの関門: gremlins の結果の行 (Killed: …, Lived: …, Not covered: …) がない (gremlins が途中で止まった?)" >&2
  exit 1
fi
k=$(printf '%s\n' "$line" | sed -E 's/.*Killed: ([0-9]+).*/\1/')
l=$(printf '%s\n' "$line" | sed -E 's/.*Lived: ([0-9]+).*/\1/')
n=$(printf '%s\n' "$line" | sed -E 's/.*Not covered: ([0-9]+).*/\1/')
if [ $((k + l + n)) -eq 0 ]; then
  echo "ok: ミューテーション 試す変異がない (Killed 0, Lived 0, Not covered 0)"
  exit 0
fi
fail=0
if [ "$n" -gt 0 ]; then
  echo "ミューテーションの関門: テストが通らない変異が $n 個 (mutant coverage 100% が必要)。上の一覧の NOT COVERED の行にテストを書く" >&2
  fail=1
fi
if [ $((k * 100)) -lt $((min * (k + l))) ]; then
  echo "ミューテーションの関門: 殺せた変異が $k / $((k + l)) (test efficacy ${min}% 以上が必要)。上の一覧の LIVED の行を確かめるテストを書く" >&2
  fail=1
fi
[ "$fail" -eq 0 ] && echo "ok: ミューテーション Killed $k, Lived $l, Not covered $n"
exit "$fail"
