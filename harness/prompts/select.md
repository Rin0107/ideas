# select — アイディアを 1 つ選び、repo と issue を作る

`wk next` の `reason` が「選定の途中から再開」なら、手順 4 から。

**主力のアプリを始めるとき** (`reason` が「主力のアプリ (…) を始める」、docs/DESIGN-v3.md §10): 候補は主力の 1 件だけ (`wk candidates` がそれだけを出す)。
採点はその 1 件で、`stack` は `"web-db"`、`needs_server`・`sensitive` はありのままに書いてよい (持ち主が決めた例外。wk は足切りしない)。
計画 (手順 3) は `templates/web-db/CLAUDE.md` を読んでから書く。認証・DB・写真の受け取りなど雛形の守りの部分は作り直さず、
その上に体験の核を作る issue にする。本番は `https://<repo_name>.<サブドメイン>.workers.dev/` の直下 (`/<名前>/` の下ではない)

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
wk が足切りと合計点で 1 件選ぶ。全部足切りなら、ここで終える (result ok、note は「候補が全部足切り」。次の実行で別の候補が出る)。

## 3. 計画を書く
選ばれたアイディアの idea.md を読み直し、**体験の核だけ** を作る MVP を決める。計画を JSON にする:
```json
{"repo_name": "regex-crossword", "description": "毎朝 1 問、正規表現のクロスワード",
 "topics": ["typescript", "cloudflare-workers", "puzzle"],
 "goal": "今日の 1 問を開いて解き、正解が分かる",
 "issues": [
   {"title": "今日の 1 問を解ける", "body": "…", "ac": ["トップを開くと 5×5 の盤面と縦横のヒントが表示される", "…"]}
 ]}
```
- **v3: 最初の版は体験の核だけの試作**。issue は 1〜3 件で、**全部を 1 回の実行 (1 時間ほど) で作り切れる大きさ**にする。
  デザインシステム・運用手順・失敗の扱いの文書は、評価で伸びると分かってから (iterate・ship の前) でよい
- 最初の issue だけで「核の最小の形」が動くようにする (評価はそれを触って、磨くか捨てるかを決める)
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
note は「選定: <idea> → <repo>」。同じ実行の次の段 (wk next) で、そのまま作り始める。
