# wakuwaku factory v4 — 考えるのは 1 日 1 回、作るのは考えずに

2026-11-19 に決めた (okuos の decisions/2026-11-19-wakuwaku-v4-planner-worker.md)。v3 (`DESIGN-v3.md`) の上に足す。`factory.json` の `v4.enabled` で切り替える。

## 0. なぜ
11-19 の夜は tick が 16 回中 7 回しか動かず (週の利用上限の目安で止まった)、1 日 899 万トークン (予算 500 万)。
kisekae-closet はわくわく 1 点あたり 458 万。どの tick も Opus が「次に何を・どう作るか」を考えるところからやり直していた。

## 1. 形
| | 計画 (考える) | 実行 (作る) |
|---|---|---|
| いつ | 毎日 23:10 に 1 回 (Orca の自動起動 `wakuwaku plan`) | 0:00〜6:30 に 30 分おき、14 回 (`wakuwaku tick`) |
| モデル | Opus | Sonnet |
| 入口 | `harness/PLAN.md` → `wk next --planner` | `harness/TICK.md` → `wk next` |
| やること | 修理・振り返り・評価 → `wk plan` で task を 20 件以上 | task を 1 件ずつ、`wk verify` が通るまで |

`wk next` (実行) は `task` か `idle` しか返さない。評価・計画・修理は全部計画の側 (`internal/next/v4.go`)。
計画の側は v3 と同じ流れで、build の代わりに plan。同じ日には 1 回だけ計画する (`Project.Planned`)。

## 2. task
issue (ラベル `wk:task`、マイルストーン v0.<n>) の本文: **目的・やること・完了条件・受け入れテスト・変えてよいファイル**。
本文は人が読む写し。wk が信じるのは `state/tasks/<repo の名前>.json` (wk だけが書く) にしまった完了条件だけ。

## 3. 完了条件は wk が判定する (`wk verify`)
1. 計画は **今は落ちる受け入れテスト** を task ごとに書く。`wk plan` は task ごとにテストだけのブランチ `wk/t<issue>` を作り、
   完了条件のコマンドを走らせ、**どれも落ちない task は登録しない** (最初から通る完了条件は意味がない)
2. 実行はそのブランチの上で作る。`wk verify` は ① 受け入れテストのファイルが計画のときのまま (sha256) ② 変えたファイルが
   「変えてよいファイル」の中 ③ コミット済み ④ 完了条件のコマンドが全部終了コード 0、を見る。通れば今のコミットを記録する
3. `wk merge` は task のブランチの PR を、`wk verify` が通ったコミットのときだけ merge する (今までの review・CI の関門もそのまま)

完了条件のコマンドは guard (`guard.Bash`) を通したものだけを登録し、鍵らしい環境変数を消して実行する。

## 4. 人の issue
持ち主は朝、工場の repo かアプリの repo に issue を書く (ラベルなし)。`wk inbox` がそれを集める (wk:・role: のラベルのない、持ち主の open な issue)。
計画は全部を task にする (`from`) か、しない理由を書く (`declined`)。漏れがあれば `wk plan` が止める。
登録のあと、wk が元の issue に「どの task にしたか / しない理由」を書いて閉じる。

## 5. 詰まったら
task の試行は 2 回まで (`v4.task_attempts`)。届いたら wk:blocked。実行は自分で task を変えず、次の task に進む。
次の夜の計画が、blocked の理由を読んで分け直す。前の計画の残りは `wk plan` が閉じる (作り直したので)。

## 6. 測るもの
- 完了した task ÷ 計画した task、blocked の割合
- task 1 件あたりのトークン、わくわく 1 点あたりのトークン (`wk usage`)
- 週の上限で飛ばされた tick の数
