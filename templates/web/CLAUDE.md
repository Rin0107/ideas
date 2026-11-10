# {{NAME}} — エージェントの決まり

wakuwaku factory が作るアプリ。工場の設計: `~/src/okuos/repos/ideas/docs/DESIGN.md`。

## 一番大事: 重要な情報を持たない

- アカウント・ログイン・Cookie・メールアドレス・名前・住所・電話・IP・User-Agent・位置・支払いを**作らない・受け取らない・残さない**
- 利用者のデータはブラウザの中 (`src/storage.ts`) だけ。キーは必ず `{{NAME}}:` で始める (ほかのアプリと同じ origin に並ぶため)。何を置くかは `DATA.md` に書く
- 本番は**静的ファイルだけ** (配信用の repo の `/{{NAME}}/` の下)。サーバーの処理・API・保存の場所は作らない。`src/worker/` は手元の確認用
- ファイルの道は相対 (`./`) か `import.meta.env.BASE_URL` から。`/` で始まる道は本番で壊れる
- Service Worker・`document.cookie`・IndexedDB は使わない (eslint が止める)
- ホーム画面に追加できる形 (PWA) は雛形に入っている: `public/manifest.webmanifest` と `public/icons/`。アイコンと色はアプリに合わせて作り直してよい (PNG 180・192・512)
- 外部のスクリプト・フォント・解析・広告を入れない。外部 API は鍵の要らない公開 API だけ (wk.json の egress_allow はユーザーだけが変える)
- 生の HTML を差し込まない。入力は zod で検査し、サイズの上限をかける
- 秘密情報を repo に置かない。鍵の要るものを作らない (鍵はどこにも用意しない)

## 正しさの決まり (CI が全部検査する。docs/TESTING.md)
- **仕様が先**: 振る舞いを変えるときは、先に `spec/` (TLA+ の状態の流れ・Z3 の性質) を書き換える。cfg の INVARIANT ごとに `"Spec_<名前>: …"` のテストで実装を検査する
- **カバレッジ 100%** (src/ の全部、行・分岐・関数・文)。`npm test` が下回ると落ちる
- **テストの種類をそろえる**: 境界値 (端とその 1 つ外)・全件ループ (小さな範囲は全部)・PBT (fast-check、回数は `tests/unit/fc.ts` の `runs()`)・fuzz (名前に `fuzz` を含むテスト。入力を受け取る関数ごとに)・M (`tests/e2e`、手元の Worker をブラウザで)・L (プレビューの URL で同じ e2e。wk deploy が起動)
- **ミューテーション** (変えたファイルだけ、Stryker): score 95% 以上。生き残った変異は、テストが足りない印 (エラーの文言・境界・分岐を確かめる)
- **速さ**: CI のジョブはどれも 5 分で打ち切り。遅いテストは小さな範囲に絞り、大きな範囲は PBT と fuzz に任せる
- 例 (`src/counter.ts`・`spec/Counter.*`) は本物を作るときに消す

## 進め方

- 1 issue = 1 ブランチ = 1 PR (`Closes #N`)。main に直接 push しない
- issue の受け入れ条件ごとにテストを書く。テスト名に `AC-<issue>-<n>` を入れる (`scripts/ac-coverage.mjs` が CI で検査)
- `npm run verify` が通ってから PR。プレビューは `wk deploy` (工場がやる)。`wk review` → `wk merge` (gh pr merge は使わない)
- 依存を足すのは最後の手段。足すなら `npm install -E` で固定し、PR に理由を書く

## ロールと持ち場の文書 (工場の docs/ROLES.md)
変えるときは、同じ PR で持ち場の文書も直す。レビューはロールごと (sre・engineer、画面に触れたら designer)。
- `docs/design-system.md` — **designer**: トークン・部品・状態・文言・アクセシビリティ。画面はここに書いたものだけで作る
- `docs/architecture.md` — **engineer**: ドメインの言葉・層と依存の向き・仕様との対応
- `docs/runbook.md` — **sre**: 動かし方・失敗の種類と扱い・戻し方・テストの体制

## 変えてはいけないもの (guard が止める)

`.github/` `.claude/` `scripts/` `src/worker/security.ts` `wk.json` `vitest.config.ts` `stryker.config.json` `spec/README.md`
