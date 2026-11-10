#!/bin/sh
# ミューテーションテスト: BASE からの変更 (テスト以外の .go) にだけ、壊したコードを作ってテストが気づくかを見る。
# 変更がなければ何もしない。初めての push (BASE が 0000…) は空の木と比べる (全部が対象)。
# 守りの部分 (wakuwaku guard で書き換え禁止)。
set -eu
base="${1:?比べる元 (コミット) を渡す}"
case "$base" in
  ""|0000000000000000000000000000000000000000) base=$(git hash-object -t tree /dev/null) ;;
esac
changed=$(git diff --name-only "$base" -- '*.go' ':!*_test.go' || true)
if [ -z "$changed" ]; then
  echo "ok: 変わった .go がない (ミューテーションは不要)"
  exit 0
fi
echo "対象:"; printf '  %s\n' $changed
exec make mutate BASE="$base"
