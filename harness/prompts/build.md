# build — 今のマイルストーンの issue を全部、1 つの PR で作り切る (v3)

`wk next` の `repo` と `issues` が対象 (`issue` はその先頭)。アプリの main の checkout は `~/src/wakuwaku/<repo の名前>`。
v3 では **1 回で版を作り切る**。issue ごとに PR とレビューを回さない (継ぎ目ごとに手間と失敗が出るため。docs/DESIGN-v3.md §0)。
`fix: true` (直す issue) のときは、その 1 件だけを直す。

## 1. 準備
```bash
APP=~/src/wakuwaku/<名前>
git -C $APP fetch origin
git -C $APP worktree add ~/src/wakuwaku/.wt/<名前>-<先頭の issue> -b wk/v-<先頭の issue> origin/main
cd ~/src/wakuwaku/.wt/<名前>-<先頭の issue>
gh issue view <N> --repo <repo>      # issues の全部
```
アプリの `CLAUDE.md` と `DATA.md`、idea.md の「体験の核」を読む。
前の試行のコメント (`wakuwaku factory: 試行 … が失敗`) があれば、その理由を読んで同じ失敗をしない。

## 2. 作る
- **体験の核が動くことを最初に**。核に効く issue から順に作る。見た目・文言・使い心地も自分で決める (designer の目)
- issue の範囲だけ。AC ごとにテストを書き、テスト名に `AC-<issue>-<k>` を入れる (CI の ac-coverage が検査する)
- 試作の段階 (ship の前) は **軽い関門だけ**: lint・型・AC のテスト・ビルド・守りの検査 (`npm run verify` / `make verify`)。
  カバレッジ 100%・ミューテーション・TLA+/Z3・fuzz は ship の前に回す (docs/DESIGN-v3.md §5)。spec/ は書かなくてよい
- **重要な情報を持たない**。サーバーに何かを置く・外と通信するなら、まず DATA.md を更新し、本当に要るかを考え直す
- 依存はなるべく足さない。足すなら `npm install -E` / `go get` し、PR に理由を書く

## 3. 確かめる
```bash
npm ci && npm run verify          # web
make verify                       # go-cli
```
通るまで直す。**テストを消す・弱める・skip して通すのは禁止**。
自分で触って確かめる: web は `npm run dev` を Playwright で開く、CLI は README どおりに動かす。核が動かないなら PR にしない。

## 4. PR → レビュー → merge (1 回だけ)
```bash
git add -A && git commit -m "<版の goal> (#a #b #c)"
git push -u origin wk/v-<先頭の issue>
gh pr create --repo <repo> --base main --title "<版の goal>" --body "Closes #a
Closes #b

<何をしたか 3〜5 行。依存を足したら理由。DATA.md を変えたら何を>"
wk review <repo> <PR 番号>
```
- `wk review` は sre の目 (セキュリティと壊れ方) で見る。守りの部分に触れたら guardian も加わる
- `request_changes` なら全部直して 1 回で push し、もう一度 `wk review`。**最大 2 回**
- approve されたら `wk merge <repo> <PR 番号>` (CI を待ち、全部 green なら merge)
- CI が赤なら `gh run view --repo <repo> --log-failed` を読んで直し、push → `wk review` からやり直す (この段の中で 1 回まで)
- 原因がこの版のコードでなく **CI・雛形・工場の側** なら、`result: fix` で報告する (工場の問題なら `--in factory`)。detail にエラーの文そのままと直し方
- `.claude/` と `wk.json` は工場が変えられない。それを変えるしかないときだけ `result: human`

CI が始まらない (run が `queued` のまま jobs 0 で 15 分以上) なら、PR を閉じて開き直す:
`gh pr close <PR 番号> --repo <repo> && gh pr reopen <PR 番号> --repo <repo>`。そのあと `wk merge` をやり直す。

## 5. 出して片付ける
```bash
cd $APP && git pull --ff-only origin main
wk deploy                      # プレビュー (CLI はリリース) に出して検査する (鍵は要らない)
git -C $APP worktree remove ~/src/wakuwaku/.wt/<名前>-<先頭の issue>
```
`wk deploy` が落ちたら `result: fix` (detail に出力)。

## 6. 作り切れなかった issue
merge した PR に入らなかった issue ごとに:
```bash
wk attempt <N> --note "<なぜ作れなかったか。次の試行への申し送り>"
```
PR が merge できたら `result: ok` (残りは次の実行が作る)。1 つも merge できなければ `result: retry`。
