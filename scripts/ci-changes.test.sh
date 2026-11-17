#!/bin/sh
# 守りの部分。scripts/ci-changes.sh の自己検査: 変わったファイルごとに、動かすジョブが正しく決まるか。
# 使い方: sh scripts/ci-changes.test.sh (CI の test で毎回流す)
set -eu
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"
git init -q -b main
git -c user.name=ci -c user.email=ci@example.com commit -q --allow-empty -m base
base=$(git rev-parse HEAD)
fail=0

# check <説明> <変えるファイル (空白区切り、- なら何も変えない)> <イベント> <PR から来たか> <期待 (名前=値 を空白区切り)>
check() {
  git reset -q --hard "$base"
  if [ "$2" != - ]; then
    for f in $2; do mkdir -p "$(dirname "$f")"; echo x > "$f"; done
    git add -A
    git -c user.name=ci -c user.email=ci@example.com commit -q -m change
  fi
  got=$(sh "$here/ci-changes.sh" "$3" "$base" "$4")
  for want in $5; do
    if ! printf '%s\n' "$got" | grep -qx "$want"; then
      echo "NG: $1: $want のはず。出力:" >&2
      printf '%s\n' "$got" | sed 's/^/  /' >&2
      fail=1
    fi
  done
}

all_off="code=false mutation=false formal=false fuzz=false template_go=false template_web=false template_web_db=false"

check "状態だけ" "state/ticks.jsonl ideas_wakuwaku/00001-x/idea.md reports/a.md" push false "$all_off"
check "文書だけ" "docs/DESIGN.md README.md" pull_request false "$all_off"
check "何も変わらない夜" - schedule false "$all_off"
check "Go を変えた PR" "cmd/wk/main.go" pull_request false \
  "code=true mutation=true formal=true fuzz=false template_go=false template_web=false template_web_db=false"
check "Go を変えた PR の merge" "internal/x/y.go" push true \
  "code=true mutation=false formal=false fuzz=false template_go=false"
check "Go を直に push" "internal/x/y.go" push false "code=true mutation=true formal=true fuzz=false"
check "Go が変わった夜" "cmd/wk/main.go" schedule false "code=true mutation=false formal=false fuzz=true"
check "雛形 web" "templates/web/src/a.ts" pull_request false \
  "code=true mutation=false formal=false template_go=false template_web=true template_web_db=false"
check "雛形 web-db の仕様" "templates/web-db/spec/A.tla" pull_request false \
  "formal=true template_web=false template_web_db=true"
check "雛形を作るスクリプト" "scripts/instantiate.sh" pull_request false \
  "code=true template_go=true template_web=true template_web_db=true mutation=false"
check "ハーネス" "harness/prompts/build.md" pull_request false "code=true mutation=false formal=false template_web=false"
check "CI 自身" ".github/workflows/ci.yml" pull_request false \
  "code=true mutation=true formal=true template_go=true template_web=true template_web_db=true fuzz=false"
check "CI 自身を手で" "scripts/ci-changes.sh" workflow_dispatch false "fuzz=true mutation=true"

# 比べる元がない (初めての push・手で全部)
git reset -q --hard "$base"
got=$(sh "$here/ci-changes.sh" workflow_dispatch "")
for want in code=true mutation=true formal=true fuzz=true template_go=true template_web=true template_web_db=true; do
  printf '%s\n' "$got" | grep -qx "$want" || { echo "NG: 比べる元なし: $want のはず" >&2; fail=1; }
done
got=$(sh "$here/ci-changes.sh" push 0000000000000000000000000000000000000000 false)
printf '%s\n' "$got" | grep -qx "mutation=true" || { echo "NG: 0000… は全部のはず" >&2; fail=1; }

[ "$fail" -eq 0 ] && echo "ok: ci-changes の自己検査"
exit "$fail"
