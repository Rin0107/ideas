# {{NAME}} — エージェントの決まり

wakuwaku factory が作るアプリ。工場の設計: `~/src/okuos/repos/ideas/docs/DESIGN.md`。

## 一番大事: 重要な情報を持たない

- アカウント・ログイン・Cookie・メールアドレス・名前・住所・電話・IP・User-Agent・位置・支払いを**作らない・受け取らない・残さない**
- 利用者のデータはブラウザの中 (`src/storage.ts`) だけ。サーバーに置くのは匿名の集計だけ。置く前に `DATA.md` を更新する
- 外部のスクリプト・フォント・解析・広告を入れない。外部 API は鍵の要らない公開 API だけ (wk.json の egress_allow はユーザーだけが変える)
- 生の HTML を差し込まない。入力は zod で検査し、サイズの上限をかける
- 秘密情報を repo に置かない。Worker に secret を持たせない

## 進め方

- 1 issue = 1 ブランチ = 1 PR (`Closes #N`)。main に直接 push しない
- issue の受け入れ条件ごとにテストを書く。テスト名に `AC-<issue>-<n>` を入れる (`scripts/ac-coverage.mjs` が CI で検査)
- `npm run verify` が通ってから PR。`wk review` → `wk merge` (gh pr merge は使わない)
- 依存を足すのは最後の手段。足すなら `npm install -E` で固定し、PR に理由を書く

## 変えてはいけないもの (guard が止める)

`.github/` `.claude/` `scripts/` `src/worker/security.ts` `wk.json`
