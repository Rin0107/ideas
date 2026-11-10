# {{NAME}} — エージェントの決まり

wakuwaku factory が作るアプリ。工場の設計: `~/src/okuos/repos/ideas/docs/DESIGN.md`。

## 一番大事: 重要な情報を持たない

- アカウント・ログイン・Cookie・メールアドレス・名前・住所・電話・IP・User-Agent・位置・支払いを**作らない・受け取らない・残さない**
- 利用者のデータはブラウザの中 (`src/storage.ts`) だけ。キーは必ず `{{NAME}}:` で始める (ほかのアプリと同じ origin に並ぶため)。何を置くかは `DATA.md` に書く
- 本番は**静的ファイルだけ** (配信用の repo の `/{{NAME}}/` の下)。サーバーの処理・API・保存の場所は作らない。`src/worker/` は手元の確認用
- ファイルの道は相対 (`./`) か `import.meta.env.BASE_URL` から。`/` で始まる道は本番で壊れる
- Service Worker・`document.cookie`・IndexedDB は使わない (eslint が止める)
- 外部のスクリプト・フォント・解析・広告を入れない。外部 API は鍵の要らない公開 API だけ (wk.json の egress_allow はユーザーだけが変える)
- 生の HTML を差し込まない。入力は zod で検査し、サイズの上限をかける
- 秘密情報を repo に置かない。鍵の要るものを作らない (鍵はどこにも用意しない)

## 進め方

- 1 issue = 1 ブランチ = 1 PR (`Closes #N`)。main に直接 push しない
- issue の受け入れ条件ごとにテストを書く。テスト名に `AC-<issue>-<n>` を入れる (`scripts/ac-coverage.mjs` が CI で検査)
- `npm run verify` が通ってから PR。プレビューは `wk deploy` (工場がやる)。`wk review` → `wk merge` (gh pr merge は使わない)
- 依存を足すのは最後の手段。足すなら `npm install -E` で固定し、PR に理由を書く

## 変えてはいけないもの (guard が止める)

`.github/` `.claude/` `scripts/` `src/worker/security.ts` `wk.json`
