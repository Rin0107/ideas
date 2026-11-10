#!/bin/sh
# 雛形からアプリを 1 つ作る (wk scaffold の copyTemplate と同じ置き換え)。工場の CI が雛形そのものを検査するのに使う。
# 使い方: sh scripts/instantiate.sh <templates/web|templates/go-cli> <出力先> <名前>
set -eu
src="$1"; dst="$2"; name="$3"
mkdir -p "$dst"
(cd "$src" && tar --exclude=node_modules --exclude=dist -cf - .) | (cd "$dst" && tar -xf -)
cd "$dst"
grep -rlZ '{{' . | xargs -0 sed -i \
  -e "s|{{NAME}}|$name|g" -e "s|{{DESCRIPTION}}|雛形の検査|g" -e "s|{{REPO}}|Rin0107/$name|g" \
  -e "s|{{GOAL}}|雛形が動く|g" -e "s|{{IDEA}}|00000-template|g" -e "s|{{URL}}|(まだ公開していません)|g"
# ミューテーションが差分を見るので git の repo にする (git のない環境では飛ばす)
if command -v git >/dev/null; then
  git init -q -b main
  git -c user.name=ci -c user.email=ci@example.com add -A
  git -c user.name=ci -c user.email=ci@example.com commit -q -m "雛形から"
fi
