# {{NAME}} — エージェントの決まり

wakuwaku factory が作る Go の CLI。工場の設計: `~/src/okuos/repos/ideas/docs/DESIGN.md`。

## 一番大事: 重要な情報を持たない
- アカウント・トークン・メールアドレス・名前・住所・IP・位置を**作らない・受け取らない・残さない・送らない**
- 保存するなら `os.UserConfigDir()` の下に、このツールの設定だけを権限 0600 で。保存する前に `DATA.md` を更新する
- 外部への通信は鍵の要らない公開 API だけ (wk.json の egress_allow はユーザーだけが変える)。解析・テレメトリは入れない
- シェルや curl を呼ばない。ファイルのパスは利用者の入力をそのまま使わず `filepath.Clean` して範囲を確かめる
- 秘密情報を repo に置かない

## 正しさの決まり (docs/TESTING.md)

**v3: 関門は段階に合わせる** (工場の docs/DESIGN-v3.md §5)。
- **試作のあいだ (ship の前)**: lint・型・AC のテスト・ビルド・守りの検査 (`make verify` / `npm run verify`)。セキュリティの決まり (上) はいつも全部
- **公開の前 (ship)**: 下の決まりを全部 (`make verify-ship` / `npm run verify-ship`、CI の fuzz・mutation・formal を手で起動)

公開の前にかける決まり:
- **仕様が先**: 振る舞いを変えるときは、先に `spec/` (TLA+ の状態の流れ・Z3 の性質) を書き換える。cfg の INVARIANT ごとに `TestSpec_<名前>` で実装を検査する
- **カバレッジ 100%** (S と M を合わせて、main も含む)。起きないはずのエラーは、テストの中なら `must()` で panic させてよい
- **テストの種類をそろえる**: 境界値 (端とその 1 つ外)・全件ループ (小さな範囲は全部)・PBT (rapid)・fuzz (入力を受け取る関数ごとに `FuzzXxx`)・M (`//go:build medium`、`TestM_`、実行ファイルを動かす)
- **ミューテーション** (変えたコードだけ): efficacy 95% 以上・mutant coverage 100%。生き残った変異は、テストが足りない印
- **速さ**: CI のジョブはどれも 5 分で打ち切り。遅いテストは小さな範囲に絞り、大きな範囲は PBT と fuzz に任せる
- 例 (`counter.go`・`spec/Counter.*`) は本物を作るときに消す

## 進め方
- 1 版 = 1 ブランチ = 1 PR (`Closes #N` を issue の数だけ並べる)。main に直接 push しない
- issue の受け入れ条件ごとにテストを書く。テスト名かコメントに `AC-<issue>-<n>` を入れる (CI が検査)
- `make verify` が通ってから PR。`wk review` → `wk merge` (gh pr merge は使わない)
- 依存は標準ライブラリを優先。足すなら PR に理由を書く (直接の依存は 8 個まで)

## ロールと持ち場の文書 (工場の docs/ROLES.md)
変えるときは、同じ PR で持ち場の文書も直す。レビューは sre の目 (セキュリティと壊れ方)。画面の良し悪しは評価で見る (v3)。
- `docs/design-system.md` — **designer**: コマンドと引数の名前・出力の形・色の使い方・エラーの文・終了コード
- `docs/architecture.md` — **engineer**: ドメインの言葉・層と依存の向き・仕様との対応
- `docs/runbook.md` — **sre**: 動かし方・失敗の種類と扱い・戻し方・テストの体制

## 変えてはいけないもの (guard が止める)
`.github/` `.claude/` `scripts/` `wk.json` `Makefile` `spec/README.md`
