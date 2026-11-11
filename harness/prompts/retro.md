# retro — 週 1 回、ふりかえって手順を直す

## 1. 集計する
`state/ticks.jsonl` の前回の retro 以降の分を読み、数える:
- 段階ごとの tick 数と成功・失敗、aborted
- 失敗の note を種類に分ける (CI・レビュー・デプロイ・時間切れ・その他)
- 作っているアプリの issue: 閉じた数・blocked の数 (`gh issue list --repo <repo> --state all --label wk:task`)
- `reports/weekly/notes.md` に溜まった申し送り、工場の `wk:fix` と `wk:human` の issue (`gh issue list --repo <工場> --label wk:fix --state all`)
- **ロールごとの傾向**: ticks の `role` で分けて、ロールごとの tick 数・成功率・よく出る失敗の note・見回り (`patrol`) の提案の数と、
  仕分け (`triage`) で now / later / reject になった割合 (`gh issue list --repo <repo> --state all --label wk:later` など)

## 2. 週のまとめを書く
`reports/weekly/<今日>.md`:
- 今週やったこと (選定・公開・破棄)、数字 (tick・成功率・閉じた issue)
- うまくいかなかったことの上位 3 つと、その原因
- ロールごとの傾向 (どのロールがどこで詰まるか・提案が役に立ったか)
- **人に頼んだこと** (権限・接続・鍵の更新・Worker の削除など、許可が要るものだけ)。1 件ずつ `wk human --title ... --detail <ファイル>` で頼む (メールが届く)。ここにはその issue の番号を書く。なければ「なし」
- セキュリティで気になったこと (reviewer の `[security]` の指摘、guard に止められた操作の傾向)

## 3. 手順を直す (直してよいのはここだけ)
直してよい: `harness/prompts/*.md`、`templates/`、`docs/`、`harness/TICK.md`・`harness/rubric.md` (守りの部分は `reviewable_paths` にあるものだけ。`wk sync` で守りのレビューが見る)。
ロールの決まり (`.claude/agents/`・`harness/reviewers/`) と制御 (`cmd/` `internal/` `factory.json`) は工場が変えられない。直すべきだと思ったら `wk human` で頼む。
- 失敗の上位 3 つのうち、手順書や雛形を直せば防げるものだけを直す。1 回の retro で 3 か所まで
- `reviewable_paths` にない守りの部分は直さない。直すべきだと思ったら `wk human` で頼む (貼れる差分を書く)
- 直したら `reports/weekly/notes.md` の該当行を消す

## 4. 終える
```bash
CGO_ENABLED=0 GOMAXPROCS=2 sh scripts/limit.sh 2G 2 go test -p 2 ./cmd/... ./internal/... && wk check
```
leader に報告する。note は「retro: 直した <n> か所」。
