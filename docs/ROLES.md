# ROLES — 4 つのロールと、工場のループ

守りの部分 (エージェントは変えられない)。決まりのコードは `internal/role`・`internal/next`・`cmd/wk/roles.go`。

## ロール

| ロール | 使命 | 持ち場 (アプリごと) | 定義 |
|---|---|---|---|
| **leader** | 指示を出す。段ごとに担当へ渡し、短い報告だけを受け取る。文脈を節約する (手順書もコードも読まない) | 流れそのもの (`wk next` → `wk done`) | `harness/TICK.md` |
| **engineer** | 問題をエンジニアリングと良い設計で解く (クリーンアーキテクチャ・ドメイン駆動設計・仕様が先) | `docs/architecture.md` | `.claude/agents/engineer.md` |
| **designer** | サービスに合ったデザインシステムを一貫させ、UX を最高の質に。コードではなく使って判断する | `docs/design-system.md` | `.claude/agents/designer.md` |
| **sre** | QA・信頼性・セキュリティ。持続できる・堅い・安全・安定。**セキュリティは何よりも優先** | `docs/runbook.md`・テストの体制・公開 | `.claude/agents/sre.md` |

## 誰がどの段階を持つか (`wk next` の `role`)

| 段階 | 担当 | 決まり |
|---|---|---|
| `build` | issue の `role:<名前>` のラベル (なければ engineer) | `role.ForIssue` |
| `patrol` | 番のロール (sre → designer → engineer の順) | `next.patrolDue` |
| `ship` | sre | `role.ForPhase` |
| `evaluate` | leader が自分でまとめる (採点は designer: wakuwaku・weakest、sre: core_works・quality・bugs) | `role.ForPhase`・evaluate.md |
| `select` `triage` `iterate` `drop` `intake` `retro` `idle` | leader (fork に手順書をやらせる) | `role.ForPhase` |

## ループの形

```
 Orca automation (1 日に何回か)
   │ wk due (STOP・機械の余裕・1 日の上限・ロック)
   ▼
 leader ──▶ wk next ──(idle / エラー)──▶ 終わり
   ▲          │ {phase, role, ...}
   │          ▼
   │      role のサブエージェントが 1 段 (harness/prompts/<phase>.md)
   │          │ 報告 (result: ok|fail / note: 1 行)
   │          ▼
   └─── wk done → git commit → wk sync

 build の中のレビュー:
   PR ──▶ wk review ──▶ sre ─▶ engineer ─▶ (画面や表示に触れたら) designer   (1 人ずつ、別の claude -p)
            ▲                     │ 全員 approve → wk:reviewed → wk merge
            └── 直して push ◀────┘ だれかが request_changes (最大 2 回)

 仕事の見つけ方:
   patrol (ロールが持ち場を見る) ──▶ wk propose (wk:proposal) ──▶ triage (leader)
        now → 今のマイルストーンの wk:task → build
        later → wk:later → 次の iterate の計画の材料
        reject → 閉じる
```

提案があれば、見回りや build より先に仕分ける (`spec/Factory.tla` の `TriageFirst`)。
見回りと仕分けも、作る段階の tick (`ticks.phase`) を使う。だから時間切れは必ず来て、アプリは必ず評価に進む。

## 上限と、どこで守らせるか

| 上限 | 値 (`factory.json`) | 守らせる所 |
|---|---|---|
| 機械の余裕 (メモリ・メモリ待ち・CPU・cgroup の残り) | `internal/room` | `wk due`・`wk next` (足りなければ idle) |
| 1 日の tick 数 | `ticks_per_day` 12 | `wk due`・`wk next` (idle) |
| 1 回の実行で進める段の数 | `steps_per_run` 3 (0 は上限なし) | `wk next` (worktree で実行を見分ける。idle) |
| 作る段階の tick (build・patrol・triage) | `max_ticks_mvp` 30・`max_ticks_iteration` 15 | `wk next` (超えたら evaluate) |
| 見回りの間隔・始める時期 | `patrol_every_days` 3・`patrol_min_ticks` 5 | `wk next` |
| 1 回の見回りの提案の数 | `max_proposals` 3 | `wk propose` (見回りの tick の中でだけ動く) |
| open な issue の数 | `max_open_issues` 8 | `wk triage` (now を足すと超えるなら止める) |
| 1 つの issue の失敗 | `max_attempts_per_issue` 3 | `wk attempt` (超えたら wk:blocked) |
| レビューのやり直し | 2 回 | `harness/prompts/build.md` |
| 連続の失敗 | `max_consecutive_failures` 3 | `wk done` (STOP と incident) |
| ロールの名前 | engineer・designer・sre | 計画・提案の検査 (`issueErrors`) |

## 誰が何を変えてよいか

- **wk だけ**: `state/`、merge・公開・デプロイ・repo の作成と archive、issue のラベルの付け替え (仕分け)
- **ロール**: アプリのコード (PR で。全員のレビューを通す)、自分の持ち場の文書、提案 (`wk propose` で)
- **leader**: 工場の repo へのコミットと `wk sync`、仕分けの結論
- **人だけ**: 守りの部分 (`factory.json` の `protected_paths`。このファイル・`.claude/agents/`・`harness/reviewers/` を含む)

## 手元の機械のメモリとの関係

- サブエージェントは **1 つずつ**。leader は並べて動かさない (evaluate の採点も designer のあとに sre)
- reviewer はロールごとに別の `claude -p` のプロセスで、**1 人ずつ順に** 動く (`wk review`)
- 重いコマンドは雛形の `make` / `npm run` と `scripts/limit.sh` を通す (docs/TESTING.md の「手元の機械を守る」)
- 1 回の実行の段の数 (`steps_per_run`) と機械の余裕を `wk next` が毎回確かめるので、長い実行でも途中で止まれる

## まだ作っていないもの

- 公開済み (shipped) のアプリの手入れ (見回り・依存の更新)。今は作っている途中のアプリだけを見回る
- `docs/DESIGN-v2.md` の汎用のグラフの実行器。今のループは `internal/next` の決まった順番で動く
