#!/bin/sh
# 守りの部分。CI のどのジョブを動かすかを、変わったファイルから決める (docs/TESTING.md「CI の時間」)。
# 出力は GITHUB_OUTPUT の形 (名前=true|false)。引数: <イベント> <比べる元> [<PR から来た push か: true|false>]
# - 比べる元が空・0000… なら全部を変わったとみなす (初めての push・手で全部流すとき)
# - CI 自身 (ci.yml・このスクリプト) が変わったら全部を動かす
# - PR の merge で入った push と夜の定期実行は、PR の CI が重い検査を済ませているので、重いもの (heavy) を動かさない (夜は fuzz だけ)
set -eu
event="$1"
base="${2-}"
from_pr="${3:-false}"

case "$base" in 0000000000000000000000000000000000000000) base="" ;; esac

if [ -n "$base" ]; then
  files=$(git diff --name-only "$base" HEAD)
else
  files="ALL"
fi
# 何も変わっていない (夜の定期実行で 24 時間なにもなかった) なら、何も動かさない
[ -z "$files" ] && files="NONE"

has() { # has <正規表現>: どれかのファイルが合えば true
  if [ "$files" = "ALL" ]; then echo true; return; fi
  if printf '%s\n' "$files" | grep -Eq "$1"; then echo true; else echo false; fi
}

ci=$(has '^(\.github/workflows/ci\.yml|scripts/ci-changes\.sh)$')
# データだけ (状態・アイディア・記録・文書) の変更なら、Go のテストは要らない (wk check と security だけ)
data='^(state|ideas|ideas_wakuwaku|reports|journal|reviewed|docs)/|^(README\.md|SCORES\.md|SELECTION\.md|scores\.csv)$'
if [ "$files" = "ALL" ]; then
  code=true
elif [ "$files" != NONE ] && printf '%s\n' "$files" | grep -Evq "$data"; then
  code=true
else
  code=false
fi
gocode=$(has '^(cmd|internal)/|^go\.(mod|sum)$')
inst='|^scripts/instantiate\.sh$'
tgo=$(has "^templates/go-cli/$inst")
tweb=$(has "^templates/web/$inst")
twebdb=$(has "^templates/web-db/$inst")
spec=$(has '^spec/|^templates/[^/]+/spec/|^scripts/formal\.sh$|^(cmd|internal)/')

heavy=true
[ "$event" = push ] && [ "$from_pr" = true ] && heavy=false
# 夜の定期実行は fuzz と依存の脆弱性だけ (ほかは push と PR で済んでいる)
[ "$event" = schedule ] && heavy=false

or() { [ "$1" = true ] || [ "$2" = true ] && echo true || echo false; }
and() { [ "$1" = true ] && [ "$2" = true ] && echo true || echo false; }

echo "code=$(or "$code" "$ci")"
echo "mutation=$(and "$heavy" "$(or "$gocode" "$ci")")"
echo "formal=$(and "$heavy" "$(or "$spec" "$ci")")"
echo "template_go=$(and "$heavy" "$(or "$tgo" "$ci")")"
echo "template_web=$(and "$heavy" "$(or "$tweb" "$ci")")"
echo "template_web_db=$(and "$heavy" "$(or "$twebdb" "$ci")")"
# fuzz は PR と push では動かさない。夜の定期実行 (その日に Go が変わったときだけ) と手で流すときだけ
case "$event" in
  schedule | workflow_dispatch) echo "fuzz=$(or "$gocode" "$ci")" ;;
  *) echo "fuzz=false" ;;
esac
