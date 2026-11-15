# ship — 公開の点検をして、public にする

sre の目で見る (公開は SRE の責任)。安全だと言い切れないものは出さない (`result: fix` で終え、理由を note と detail に。wk が直す issue を立てる)。

public にすると、コードと履歴が誰でも読めるようになる。**秘密情報と重要な情報が無いことを、もう一度確かめる**。

## 1. 仕上げる (アプリの worktree で、PR にして wk review → wk merge)
- README.md: `(まだ公開していません)` を公開時の URL (`wk status` の「公開時の URL」。CLI はリリースの URL) に、使い方を 30 秒で分かる形に。スクリーンショットは載せない (画像のファイルを足すなら repo に置く)
- DATA.md: 実際に持っているものと一致しているか、コードを読んで確かめる
- description と topics (`gh repo view <repo>`)。空なら `gh repo edit <repo> --description … --add-topic …`

## 1.5. 重い関門を通す (v3: 試作のあいだは省いていたもの。docs/DESIGN-v3.md §5)
public にするのは、伸びると分かったものだけ。ここで初めて、正しさの決まり (docs/TESTING.md) を全部かける:
- web: `npm run verify-ship` (カバレッジ 100%)。go-cli: `make verify-ship` (S と M のカバレッジ 100%)
- CI の重い検査 (fuzz・mutation・formal) を main で動かして green を確かめる:
  `gh workflow run ci.yml --repo <repo> --ref main`、そのあと `gh run list --repo <repo> --workflow ci.yml --limit 1` が success になるまで待つ
- 足りないテスト・仕様 (`spec/`) は手順 1 と同じ PR で足す。通らなければ `result: retry` (次の実行が続ける)

## 2. 点検する
- `gh run list --repo <repo> --branch main --limit 1` が success (gitleaks が履歴全体を検査済み)
- 履歴に、テスト用の偽物でも鍵の形をした文字列がないか: `git -C ~/src/wakuwaku/<名前> log -p | grep -nE '(api[_-]?key|secret|token|password)\s*[:=]'` を見る。あれば ship しない (`result: fix` で終え、理由を note と detail に。wk が直す issue を立てる)

## 3. 公開する
```bash
wk ship                        # web: URL は wk が決める
wk ship --url <リリースの URL>   # go-cli
```
wk が README (公開時の URL が載っているか)・LICENSE・description・topics・CI を検査し、
web はプレビューで評価したものを本番に出して、本番の守りのヘッダーと中身を確かめてから public にする。
通らなければ、言われたものを直して (手順 1) 次の tick で再挑戦 (`result: fix`)。

## 4. 工場の README に足す
工場の `README.md` の「作ったもの」の表に 1 行 (名前・URL・1 行の説明・アイディアの ID)。
TICK.md の手順 3 で記録する。note は「公開: <URL>」。
