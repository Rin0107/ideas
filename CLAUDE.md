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

## 変えてはいけないもの (guard が止める)
`factory.json` の `protected_paths` にあるもの: 制御のコード (`cmd/` `internal/`)・設定・CI・設計書・採点基準・reviewer の指示・雛形の守りの部分。
変えるべきだと思ったら、`reports/weekly/notes.md` に書く (retro がユーザーに頼む)。

## 書いてよいもの
- `ideas_wakuwaku/` のアイディアと `ideas_wakuwaku/INDEX.md` (intake・iterate・drop の手順で)
- `harness/prompts/`、`templates/` の守りの部分以外、`reports/`、`README.md` の「作ったもの」

## その他
- やり取り・記録は日本語。日付は絶対表記 (2026-11-10)
- `ideas/`・`reviewed/`・`SELECTION.md`・`SCORES.md`・`scores.csv` は以前の機械生成のアイディア集。読むだけ
- Go は `CGO_ENABLED=0` で test / build する
