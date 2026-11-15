# ideas (wakuwaku factory) — エージェントの決まり

このリポジトリは、わくわくするアイディアを貯め、1 つずつ無料で作り切って公開する **工場**。
設計: `docs/DESIGN.md`。定期実行の入口: `harness/TICK.md`。

## 一番大事
1. **セキュリティが最優先: 重要な情報を持たない**。どのアプリも、盗まれても困るものが何もない形で作る (DESIGN §7)
2. お金を使わない (無料プランだけ)
3. 工場の外 (portfolio にない repo・サービス) に触れない
4. 迷ったら安全な方。わくわくより安全、完成より安全

## 決めるのは wk、作るのは LLM
- 次に何をするか・合格か・捨てるかは `wk` (Go の CLI、`cmd/wk`) が決める。自分で判断して流れを変えない
- merge・公開・デプロイ・repo の作成と archive は wk のコマンドだけで行う (`wk merge` `wk ship` `wk deploy` `wk scaffold` `wk drop`)
- 状態 (`state/`) は wk だけが書く。手で編集しない

## 進め方 (v3、docs/DESIGN-v3.md)
- 定期実行の本体 (`harness/TICK.md`) は 1 つの Claude が段を自分でやる。1 回の実行で 1 つの版を作り切る (select → build → evaluate → …)
- 状態と記録は wk が自分の clone (`~/.local/state/wakuwaku/factory`) に書いて push する。worktree でも手元の main でも state をコミットしない
- engineer・designer・sre は「目」として使い分ける (`.claude/agents/<role>.md` は手順書の材料)。PR のレビューは sre の目 (`factory.json` の `reviewers`)
- 見回り (`patrol`) と仕分け (`triage`) は止めてある (`patrol_every_days: 0`)
- 実行は同時に `max_parallel` 本、作りかけは `max_wip` 件まで (v3.1、DESIGN-v3 §8)。`wk next` が渡したアプリだけを触る
- どれだけ動くかは Claude の利用上限 (5 時間・週) で決める (v3.2、DESIGN-v3 §9)。記録がなければ 1 日の予算

## 変えてはいけないもの (guard が止める)
`factory.json` の `protected_paths` にあるもの: 制御のコード (`cmd/` `internal/`)・設定・CI・設計書・採点基準・reviewer の指示・雛形の守りの部分。
変えるべきだと思ったら、`wk human --title ... --detail <ファイル>` で人に頼む (持ち主にメールが届く。貼れる差分を書く)。守りの部分でない工場の問題は `wk fix --title ... --detail <ファイル>` (次の tick で工場が直す)。

## 書いてよいもの
- `ideas_wakuwaku/` のアイディアと `ideas_wakuwaku/INDEX.md` (intake・iterate・drop の手順で)
- `harness/prompts/`、`templates/` の守りの部分以外、`reports/`、`README.md` の「作ったもの」

## その他
- やり取り・記録は日本語。日付は絶対表記 (2026-11-10)
- `ideas/`・`reviewed/`・`SELECTION.md`・`SCORES.md`・`scores.csv` は以前の機械生成のアイディア集。読むだけ
- Go は `CGO_ENABLED=0` で test / build する
