# build — issue を 1 つ実装して、merge・デプロイする

`wk next` の `repo` と `issue` が対象。アプリの main の checkout は `~/src/wakuwaku/<repo の名前>`。
担当は `role` (issue の `role:<名前>` のラベル。なければ engineer)。手順は同じで、**自分のロールのやり方で** 作る
(engineer: 設計と `docs/architecture.md`、designer: デザインシステムと `docs/design-system.md`、sre: 失敗の扱い・テストの体制と `docs/runbook.md`)。

## 1. 準備
```bash
APP=~/src/wakuwaku/<名前>; N=<issue>
git -C $APP fetch origin
git -C $APP worktree add ~/src/wakuwaku/.wt/<名前>-$N -b wk/issue-$N origin/main
cd ~/src/wakuwaku/.wt/<名前>-$N
gh issue view $N --repo <repo>
```
アプリの `CLAUDE.md` と `DATA.md`、idea.md の「体験の核」を読む。
同じ issue に前の試行のコメント (`wakuwaku factory: 試行 … が失敗`) があれば、その理由を読んで同じ失敗をしない。

## 2. 実装する
- issue の範囲だけ。AC ごとにテストを書き、テスト名に `AC-<N>-<k>` を入れる
- **仕様が先** (docs/TESTING.md・アプリの CLAUDE.md): 振る舞いを変えるなら、先に `spec/` (TLA+・Z3) を書き換え、
  不変条件ごとの `TestSpec_` で実装を確かめる。境界値・全件ループ・PBT・fuzz をそろえ、カバレッジは 100%
  - web: 画面の動きは `tests/e2e/`、ロジックは `tests/unit/`
  - go-cli: `_test.go` のテスト名かコメント
- **重要な情報を持たない**。サーバーに何かを置く・外と通信するなら、まず DATA.md を更新し、本当に要るかを考え直す
- 依存はなるべく足さない。足すなら `npm install -E` / `go get` し、PR に理由を書く

## 3. 確かめる
```bash
npm ci && npm run verify          # web
make verify                       # go-cli
```
通るまで直す。web で Playwright が動くなら `npm run e2e` も (動かなければ CI に任せる)。
仕様 (TLA+・Z3) とミューテーションは手元に道具がないことがある。そのときは CI の formal・mutation を見て直す
(PR の CI が赤なら手順 4 の CI の扱いに従う)。
**テストを消す・弱める・skip して通すのは禁止**。どうしても通らなければ手順 6。

## 4. PR → レビュー → merge
```bash
git add -A && git commit -m "<issue のタイトル> (#$N)"
git push -u origin wk/issue-$N
gh pr create --repo <repo> --base main --title "<タイトル>" --body "Closes #$N

<何をしたか 2〜3 行。依存を足したら理由。DATA.md を変えたら何を>"
wk review <repo> <PR 番号>
```
- `wk review` はロールごとに別の reviewer が見る (sre・engineer はいつも、画面や表示に触れたら designer も)。結果はロールごとに PR のコメントに出る。**全員が approve して初めて approve**
- `request_changes` なら、**どのロールの指摘も全部** 直して 1 回で push し、もう一度 `wk review`。**最大 2 回**。3 回目も通らなければ手順 6
- approve されたら:
```bash
wk merge <repo> <PR 番号>     # CI を待ち、全部 green なら merge
```
CI が赤なら、ログ (`gh run view --repo <repo> --log-failed`) を読んで直し、push → `wk review` からやり直す (この tick の中で 1 回まで)。
- 原因がこの issue のコードでなく、**CI・雛形・工場の側** (権限・道具の版・手順書の誤りなど) なら、ここで止めて `result: fix` で報告する。
  detail にエラーの文そのままと、どこをどう直せばよいかを書く。工場 (`templates/`・`harness/`・CI) の問題なら `in: factory` も付ける
- 直すのに **守りの部分** (アプリの `.github/workflows/` や雛形の守りの部分など) を変えるしかないなら `result: human`。detail に貼れる差分を書く
- `reports/weekly/notes.md` に書いて終わりにはしない (誰も直さず、同じ所で詰まり続ける)

## 5. デプロイして片付ける
```bash
cd $APP && git pull --ff-only origin main
wk deploy                      # プレビューに出して、守りのヘッダーと中身を検査 (鍵は要らない)
git -C $APP worktree remove ~/src/wakuwaku/.wt/<名前>-$N
```
`wk deploy` が落ちたら `result: fix` で終える (detail に出力。wk が直す issue を立て、次の tick で直す)。
leader に報告する (result ok)。

## 6. 失敗したとき
```bash
wk attempt $N --note "<なぜ通らないか。次の試行への申し送り>"
```
PR は閉じず、ブランチも残す (次の試行が読める)。worktree だけ消す。leader に報告する (`result: retry`。試行は issue に数えてあり、上限で blocked になる)。
