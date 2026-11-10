# ideas — wakuwaku factory

わくわくするアイディアを貯め、1 つ選んで **無料のサービスだけで作り切って公開し**、評価して磨くか捨てる。
これを Orca の定期実行で、人の手を介さずに回し続ける工場。

**セキュリティが最優先**: 作るアプリはどれも重要な情報を持たない (アカウントなし・データは端末の中だけ・外部への送信なし)。

- 設計: [docs/DESIGN.md](docs/DESIGN.md)
- 最初の準備 (人が 1 回): [docs/SETUP.md](docs/SETUP.md)
- 定期実行の入口: [harness/TICK.md](harness/TICK.md)
- 制御 CLI: `wk` (`cmd/wk`)。`wk status` で今の状態

## 作ったもの
| 名前 | URL | 何か | アイディア |
|---|---|---|---|

## 仕組み (要約)
```
 Orca「wakuwaku tick」(1日6回) → wk due (止める条件) → wk next (次の段階を決める)
   → harness/prompts/<段階>.md に従って 1 ステップ → wk done → wk sync
 段階: select → build (issue ごと) → evaluate → iterate / ship / drop、ほかに intake・retro
```
- 決めるのは `wk` (ルールと点数)、作るのは LLM。merge・公開・デプロイは wk だけが行う
- CI (lint・型・テスト・e2e・gitleaks・依存の脆弱性・持たない情報の検査・外部送信の検査) が全部 green で、
  別の Claude のレビューが approve のときだけ merge
- 守りの部分 (制御のコード・設定・CI・採点基準) はエージェントが書き換えられない (Claude Code のフックで強制)

| 場所 | 中身 |
|---|---|
| `ideas_wakuwaku/` | ループが使うアイディア (501 件〜)。`INDEX.md` が一覧 |
| `state/` | アプリの一覧と tick の記録 (wk だけが書く) |
| `reports/` | 評価・週のまとめ |
| `harness/` | tick の手順書・採点基準・reviewer の指示 |
| `templates/` | 雛形 (`web`: 静的な Web アプリ、`go-cli`: Go の CLI、`site`: 全 Web アプリを配る Cloudflare Worker) |
| `ideas/` ほか | 以前の機械生成のアイディア集 (下に説明。読むだけ) |

---

# 以前のアイディア集

Goでマネタイズ可能なアプリケーションを作るためのアイディア集。

## 構成 (以前のアイディア集)

```
ideas/
  idea-template/       # 新規アイディア追加時のテンプレート
    idea-template.md
  00001-log-diff-watcher/
    idea.md
  00002-git-sparse-optimizer/
    idea.md
  ...
ideas_wakuwaku/        # 「わくわくすること」を条件とした別コレクション
  idea-template/
    idea-template.md
  00001-infra-aquarium/
    idea.md
  ...
reviewed/
  round-01.md          # 各ラウンドの不採用アイディア一覧(1行1件)
  ...
  round-10.md
```

各アイディアは `ideas/{5桁ID}-{slug}/idea.md` に1つずつ格納する。
フォーマットは `ideas/idea-template/idea-template.md` を参照。
IDは検討順の通し番号(欠番あり=不採用分)。不採用分は `reviewed/round-XX.md` に記録。

## ideas_wakuwaku(わくわくコレクション)

`ideas/` の機械的な大量生成・選定とは別軸のコレクション。機械的手法は使わず、1件ずつ個別に発想する。
採用条件は「わくわくすること」。マネタイズは必須ではない(テンプレートに `わくわくポイント` セクションあり)。
IDは `ideas_wakuwaku/` 内で独立した通し番号(00001〜)。フォーマットは `ideas_wakuwaku/idea-template/idea-template.md` を参照。

## 選定プロセス(完了)

全10ラウンドで累計91,000件を検討し、各ラウンド上位1,000件・計10,000件を採用した。
スコア = 業種スコア + ツール型スコア(+ラウンド2以降は派生型スコア)。

| Round | プール | 内容 | 採用 |
|---|---|---|---|
| 1 | 00001〜10000 | 手書き150 + 機械生成9,850 | 1,000(手書き150は全採用) |
| 2 | 10001〜19000 | AI自動化特化版 | 1,000 |
| 3 | 19001〜28000 | モバイル特化版 | 1,000 |
| 4 | 28001〜37000 | API提供型 | 1,000 |
| 5 | 37001〜46000 | セルフホスト版 | 1,000 |
| 6 | 46001〜55000 | 多店舗チェーン向け | 1,000 |
| 7 | 55001〜64000 | FC本部向け | 1,000 |
| 8 | 64001〜73000 | 自治体・公共向け | 1,000 |
| 9 | 73001〜82000 | インバウンド対応特化版 | 1,000 |
| 10 | 82001〜91000 | 格安シンプル版 | 1,000 |

## 現在のフェーズ

- [x] アイディア出し・選定(検討91,000件 → 採用10,000件)
- [x] 実装優先度の選定(`SELECTION.md` — トップ3推奨あり)
- [x] 採用10,000件の採点(`SCORES.md` に分布とトップ50、`scores.csv` に全件ランキング、各idea.mdに内訳)
- [ ] Go実装(CLI/サーバーとしてプロトタイプ作成)

Go実装はアイディアごとに着手する際、各ディレクトリに `main.go` を追加していく。
