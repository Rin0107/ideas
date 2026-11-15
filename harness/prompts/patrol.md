# patrol — 自分の持ち場を見回り、仕事を提案する

`wk next` の `role` (sre・designer・engineer) が、作っているアプリ (`repo`) の自分の持ち場を見る。
見つけたことを issue の提案 (`wk:proposal`) にする。**提案なしは良い結果**。無理に探さない。

## 1. 見る (アプリの main の checkout は `~/src/wakuwaku/<repo の名前>`。読むだけ。直さない)
アプリの `CLAUDE.md`・`DATA.md`、idea.md の「体験の核」、自分の持ち場の文書 (下) を先に読む。

- **sre** (`docs/runbook.md`):
  - CI の履歴 (`gh run list --repo <repo> --limit 20`)。同じテストがときどき落ちる (不安定) か
  - 依存の注意 (`npm audit --omit=dev` / `govulncheck ./...`。雛形の `npm run` / `make` から動かす)
  - セキュリティの門 (CSP・守りのヘッダー・gitleaks・外部への通信) が効いているか。DATA.md と実際が合っているか
  - プレビュー (`wk status` の URL) と本番の確かめ。runbook に書かれていない失敗の種類
- **designer** (`docs/design-system.md`):
  - プレビューを Playwright で触る (スマホ幅 360px も)。CLI は最新のリリースを動かす
  - デザインシステムと合っているか (トークン・部品・文言の調子)。粗い状態 (空・読み込み中・失敗) がないか。文言
- **engineer** (`docs/architecture.md`):
  - 設計のずれ (ドメインと I/O・UI が混ざっている・依存の向き)。同じことの重複
  - 仕様 (`spec/`) やテストの抜け (AC のテスト・境界値・fuzz)。長すぎる・複雑すぎる関数

## 2. 提案を書く (`max_proposals` 件まで。`factory.json`)
本当に大事なものだけ。1 件 = 1 tick で終わる大きさ。
```json
{"issues": [
  {"title": "スマホ幅で盤面がはみ出す", "body": "根拠: 360px で横に 40px 流れる (Playwright で確かめた)。…",
   "ac": ["360px の幅で、盤面が横に流れない"], "role": "designer"}
]}
```
- `body` には **根拠** (何を見て・どう確かめたか) を必ず書く。根拠のない提案は出さない
- `role` は担当。省けば自分。ほかのロールの持ち場なら、そのロールにする
- 重要な情報を持つ方向の提案はしない

## 3. 出す
```bash
wk propose --plan /tmp/wk-proposals.json    # 0 件なら {"issues": []}
```
次の段で leader が仕分ける (`triage`)。

**工場そのもの** (wk・手順書・雛形・守りの部分) についての気づきは、提案にしない。
`wk fix --title ... --detail <ファイル>` で直す issue にする (次の tick で工場が直す。守りの部分は守りのレビューを通す)。人の許可 (権限・接続・鍵・お金) が要るものだけ `wk human --title ... --detail <ファイル>` (メールが届く)。

## 4. 終える
TICK.md の手順 3 で記録する (note は「<role> の見回り: 提案 <n> 件」)。
