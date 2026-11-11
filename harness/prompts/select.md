# select — アイディアを 1 つ選び、repo と issue を作る

`wk next` の `reason` が「選定の途中から再開」なら、手順 4 から。

## 1. 候補を出す
```bash
wk candidates > /tmp/wk-candidates.json   # 日付で決まる 15 件
```
候補ごとに `ideas_wakuwaku/<idea>/idea.md` を全部読む。

## 2. 採点する
`harness/rubric.md` の select の基準で、**15 件全部** を採点し、JSON の配列にする。
- `sensitive` は厳しく: 体験の核に、重要な情報をサーバーに持つ必要があるなら true。迷ったら true
- 「ブラウザの中だけで完結させる形」に変えれば成立するなら、その形で `data_plan` を書き、false にしてよい
- `needs_server` も同じ: サーバーなしの形 (ブラウザの中・日付から決まる・URL で渡す) で核が成り立つなら false。成り立たないなら true
```bash
wk select --scores /tmp/wk-scores.json
```
wk が足切りと合計点で 1 件選ぶ。全部足切りなら、ここで終えて leader に報告する (result ok、note は「候補が全部足切り」。次の tick で別の候補が出る)。

## 3. 計画を書く
選ばれたアイディアの idea.md を読み直し、**体験の核だけ** を作る MVP を決める。計画を JSON にする:
```json
{"repo_name": "regex-crossword", "description": "毎朝 1 問、正規表現のクロスワード",
 "topics": ["typescript", "cloudflare-workers", "puzzle"],
 "goal": "今日の 1 問を開いて解き、正解が分かる",
 "issues": [
   {"title": "盤面を表示する", "body": "…", "ac": ["トップを開くと 5×5 の盤面と縦横のヒントが表示される", "…"], "role": "engineer"},
   {"title": "デザインシステムを決める", "body": "…", "ac": ["docs/design-system.md に色・文字・余白のトークンが書かれ、画面がそれだけを使う"], "role": "designer"},
   {"title": "失敗したときの扱い", "body": "…", "ac": ["保存できないとき、画面に理由と次にすることが表示される"], "role": "sre"}
 ]}
```
- issue は 3〜8 件。**1 件 = 1 tick で終わる大きさ**。最初の issue で「核の最小の形」が動くようにする
- issue ごとに `role` (担当) を付ける: `engineer` (設計と実装)・`designer` (見た目・文言・使い心地)・`sre` (失敗の扱い・テスト・セキュリティ)。省けば engineer
  - 画面や表示のあるアプリ (web、表示の凝った CLI) は、早い順番に designer の issue を 1 つ: `docs/design-system.md` を決める
  - sre の issue を 1 つ: 失敗の形 (保存できない・壊れた入力・通信できない) の扱いと `docs/runbook.md`
- AC はテストにできる文で書く (「〜すると〜が表示される」)。1 件 1〜5 個
- どの issue も、DATA.md の原則を守る形で書く。ブラウザに何か置くなら、その issue の AC に「DATA.md に〜が書かれている」を入れる
- stack が web なら、静的ファイルだけで作る (サーバーの処理・API は作らない。本番は `/<名前>/` の下で動く)
- repo_name は英小文字とハイフン。既存の有名な製品名を使わない

## 4. 作る
```bash
wk scaffold --plan /tmp/wk-plan.json
```
雛形から private の repo を作り、ラベル・マイルストーン MVP・issue を登録する。
落ちたら、エラーを読んで直せるもの (repo_name が使われている・計画の形) は直して再実行。直せなければ `result: fix` で終える (detail にエラーの文)。

## 5. 終える
leader に報告する。note は「選定: <idea> → <repo>」。
