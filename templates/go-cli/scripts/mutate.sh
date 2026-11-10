#!/bin/sh
# ミューテーションテスト: BASE からの変更 (テスト以外の .go) にだけ、壊したコードを作ってテストが気づくかを見る。
# 変更がなければ何もしない。BASE が空か 0000… (初めての push) なら全部が対象。
# 守りの部分 (wakuwaku guard で書き換え禁止)。
set -eu
base="${1-}"
case "$base" in
  0000000000000000000000000000000000000000) base="" ;;
esac
if [ -n "$base" ]; then
  changed=$(git diff --name-only "$base" -- '*.go' ':!*_test.go' || true)
  if [ -z "$changed" ]; then
    echo "ok: 変わった .go がない (ミューテーションは不要)"
    exit 0
  fi
  echo "対象:"; printf '  %s\n' $changed
else
  echo "対象: 全部"
fi
exec make mutate BASE="$base"
