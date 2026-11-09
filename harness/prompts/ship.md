# ship — 公開の点検をして、public にする

public にすると、コードと履歴が誰でも読めるようになる。**秘密情報と重要な情報が無いことを、もう一度確かめる**。

## 1. 仕上げる (アプリの worktree で、PR にして wk review → wk merge)
- README.md: `(まだ公開していません)` を本番の URL に、使い方を 30 秒で分かる形に。スクリーンショットは載せない (画像のファイルを足すなら repo に置く)
- DATA.md: 実際に持っているものと一致しているか、コードを読んで確かめる
- description と topics (`gh repo view <repo>`)。空なら `gh repo edit <repo> --description … --add-topic …`

## 2. 点検する
- `gh run list --repo <repo> --branch main --limit 1` が success (gitleaks が履歴全体を検査済み)
- 履歴に、テスト用の偽物でも鍵の形をした文字列がないか: `git -C ~/src/wakuwaku/<名前> log -p | grep -nE '(api[_-]?key|secret|token|password)\s*[:=]'` を見る。あれば ship しない (fail で終え、理由を note に)

## 3. 公開する
```bash
wk ship --url <本番の URL>
```
wk が README・LICENSE・description・topics・CI・本番の守りのヘッダーを検査してから public にする。
通らなければ、言われたものを直して (手順 1) 次の tick で再挑戦 (fail で終える)。

## 4. 工場の README に足す
工場の `README.md` の「作ったもの」の表に 1 行 (名前・URL・1 行の説明・アイディアの ID)。
TICK.md の手順 3。note は「公開: <URL>」。
