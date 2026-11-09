# retro — 週 1 回、ふりかえって手順を直す

## 1. 集計する
`state/ticks.jsonl` の前回の retro 以降の分を読み、数える:
- 段階ごとの tick 数と成功・失敗、aborted
- 失敗の note を種類に分ける (CI・レビュー・デプロイ・時間切れ・その他)
- 作っているアプリの issue: 閉じた数・blocked の数 (`gh issue list --repo <repo> --state all --label wk:task`)
- `reports/weekly/notes.md` に溜まった申し送り

## 2. 週のまとめを書く
`reports/weekly/<今日>.md`:
- 今週やったこと (選定・公開・破棄)、数字 (tick・成功率・閉じた issue)
- うまくいかなかったことの上位 3 つと、その原因
- **人に頼みたいこと** (Worker の削除、守りの部分の変更の提案、鍵の更新など)。なければ「なし」
- セキュリティで気になったこと (reviewer の `[security]` の指摘、guard に止められた操作の傾向)

## 3. 手順を直す (直してよいのはここだけ)
直してよい: `harness/prompts/*.md`、`templates/` のうち守りの部分以外、`docs/` のうち DESIGN.md 以外。
- 失敗の上位 3 つのうち、手順書や雛形を直せば防げるものだけを直す。1 回の retro で 3 か所まで
- **守りの部分 (`factory.json` の `protected_paths`) は直さない**。直すべきだと思ったら、まとめの「人に頼みたいこと」に書く
- 直したら `reports/weekly/notes.md` の該当行を消す

## 4. 終える
```bash
CGO_ENABLED=0 go test ./cmd/... ./internal/... && wk check
```
TICK.md の手順 3。note は「retro: 直した <n> か所」。
