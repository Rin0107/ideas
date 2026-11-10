# wakuwaku factory — 設計

わくわくするアイディアを、**人間の手を介さずに** 「保存 → 1つ選んで作り切る → 評価して磨くか捨てる」まで回し続ける仕組み。
このリポジトリ (`ideas`) が工場 (factory) で、作ったアプリは 1 つずつ別のリポジトリになる。

- 作成: 2026-11-09
- 状態: 土台を作った段階 (定期実行はまだ登録しない。`docs/SETUP.md` の初回準備のあと)

---

## 1. 目的と、守ること

**目的**
1. わくわくするアイディアを貯める (`ideas_wakuwaku/`)
2. 1 つ取り出して、無料のサービスだけで動くものを作り切り、公開する
3. 作ったものを評価して、磨く (ブラッシュアップ) か、捨てて次へ行く
4. これを定期実行のループで、人が見なくても回す

**守ること (優先順)**
1. **セキュリティが最優先: 重要な情報を持たない**。すべてのアプリを「盗まれても困るものが何もない」形で作る (§7)。
   重要な情報がないと成り立たないアイディアは、作らない (選定で機械的に除外)
2. **お金を使わない**: 無料プランだけ。有料プランへの切り替え・ドメイン購入・課金 API は禁止 (§8)
3. **壊さない**: 工場が触れるのは、工場が作ったリポジトリとサービスだけ (§8 のガード)
4. **止まったら知らせる**: 自分で直せない失敗が続いたら、自分で止まって incident を残す (§9)
5. **記録を残す**: 何を選び、なぜ捨てたかを全部 git に残す (ユーザーの okuos の方針と同じ)

迷ったら上を優先する。わくわくより安全、完成より安全。

**人がやるのは**: 最初の準備 (アカウント連携・トークン発行) と、incident が出たときだけ。

---

## 2. 全体の形

```
 Orca automation「wakuwaku tick」(1日6回)
   │ precheck: wk due   ← 止める条件 (STOP・予算・ロック) を機械的に判定
   ▼
 Claude Code (新しい worktree)
   1. wk next          ← 次にやる「段階」を決定論で決める (LLM は決めない)
   2. harness/prompts/<段階>.md に従って 1 ステップだけやる
   3. wk done          ← 結果を state/ に記録、ロック解除
   │
   ├─ 工場 repo (この repo): アイディア・状態・手順書・制御 CLI
   └─ アプリ repo (1 アイディア = 1 repo): コード・issue・CI
        └─ 配信: Web は配信用 repo (wakuwaku-site) → Cloudflare の GitHub 連携 / CLI は GitHub Releases
```

**基本の考え方: 決めるのはプログラム、作るのは LLM**
- 「次に何をするか」「合格か」「捨てるか」は `wk` (Go の CLI) が数字とルールで決める。LLM の気分で流れが変わらない
- LLM は各段階の中身 (アイディアを書く・コードを書く・採点する) だけをやる
- LLM の出力は必ず機械で検査する (CI・テスト・JSON の形式・`wk check`)

---

## 3. 状態と段階 (state machine)

### アプリ (project) の状態
```
 selected ─▶ building ─▶ evaluating ─┬─▶ shipped (公開して終わり)
                ▲                    ├─▶ iterating ─▶ evaluating …(最大2回)
                └────────────────────┘
                                     └─▶ dropped (捨てる。理由を残す)
```
同時に作るのは **1 つだけ** (WIP = 1)。終わる (shipped / dropped) まで次を選ばない。

### 1 tick でやる段階 (`wk next` が 1 つ返す)
| 段階 | いつ選ばれる | やること | 手順書 |
|---|---|---|---|
| `retro` | 前回の retro から 7 日以上 | 失敗の記録から手順書・雛形を直す | retro.md |
| `build` | 作っているアプリに未完了の issue がある | issue を 1 つ実装して merge・デプロイ | build.md |
| `evaluate` | 未完了 issue がない、または時間切れ | 本番を触って採点。結果で ship/iterate/drop | evaluate.md |
| `iterate` | 評価が iterate | 磨く issue を作る (最大 5 件) | iterate.md |
| `ship` | 評価が ship | 公開の点検をして public にする | ship.md |
| `drop` | 評価が drop | 片付けて、捨てた理由を残す | drop.md |
| `intake` | 作っているアプリがなく、未着手のアイディアが少ない / 7 日以上 intake していない | 新しいアイディアを足し、既存のものを磨く | intake.md |
| `select` | 作っているアプリがない | 候補を採点して 1 つ選び、repo と issue を作る | select.md |

優先順は上から。判定は `internal/next` の純粋関数で、テストで固定する。

### 時間の上限 (`factory.json`)
| 項目 | 既定 | 意味 |
|---|---|---|
| `ticks_per_day` | 6 | 1 日の tick 数の上限 |
| `max_ticks_mvp` | 30 | MVP (最初の版) にかけられる tick 数。超えたら強制で evaluate |
| `max_ticks_iteration` | 15 | 1 回のブラッシュアップにかけられる tick 数 |
| `max_iterations` | 2 | ブラッシュアップの回数の上限。超えたら ship か drop |
| `max_attempts_per_issue` | 3 | 1 つの issue に失敗してよい回数。超えたら `wk:blocked` |
| `lock_ttl_minutes` | 150 | tick が落ちたときに、ロックを自動で外すまでの時間 |
| `max_consecutive_failures` | 3 | 連続失敗がこれを超えたら、自分で止まる (§9) |

---

## 4. データの置き場所

```
ideas/                 (既存) 機械生成の 10,000 件。読むだけ。今回のループの対象外
ideas_wakuwaku/        アイディア置き場。1 件 = <5桁ID>-<slug>/idea.md
  idea-template/       雛形
state/
  portfolio.json       アプリの一覧と状態 (wk だけが書く)
  ticks.jsonl          tick ごとの記録 (wk だけが書く)
  meta.json            intake・retro の最終日
  (git の外) ~/.local/state/wakuwaku/  ロック・今日の tick 数・連続失敗・STOP・approve の記録
reports/
  weekly/<日付>.md     週のまとめ (retro が書く)
  evaluations/<repo>-<回>.md  評価の記録
harness/
  prompts/<段階>.md    各段階の手順書
  rubric.md            選ぶとき・評価するときの採点基準
  reviewer.md          PR を見る別の Claude への指示
templates/
  web/                 Web アプリ (Vite + React + TS。本番は静的ファイルだけ)
  site/                配信用 repo の雛形 (wk site init が使う。全アプリを /<名前>/ で配る Worker)
  go-cli/              Go の CLI (GitHub Releases で配る)
factory.json           予算・上限・使ってよいサービス (人だけが変える)
cmd/wk, internal/      制御 CLI
```

### idea.md に足すもの
選んだ・磨いた・捨てたときは、idea.md の末尾に `## 履歴` を足して 1 行ずつ書く:
```
## 履歴
- 2026-11-12 選定: 合計 21/25 (わくわく5・軽さ4・無料4・自分ごと4・Findy4) → repo Rin0107/regex-crossword
- 2026-11-18 評価1: 核は動く・わくわく3 → iterate (「解いたあとの共有画像」を足す)
- 2026-11-22 評価2: わくわく4・品質4 → ship https://regex-crossword.<sub>.workers.dev
```
ブラッシュアップで本文 (体験の核など) を書き換えるときは、古い内容を消さず `## 履歴` に何を変えたかを書く。

### portfolio.json (1 アプリ)
```json
{
  "idea": "00141-regex-crossword",
  "repo": "Rin0107/regex-crossword",
  "status": "building",
  "stack": "web",
  "started": "2026-11-12",
  "milestone": "MVP",
  "iteration": 0,
  "ticks": {"total": 12, "phase": 12},
  "url": "",
  "history": [{"date": "2026-11-12", "event": "selected", "note": "21/25"}]
}
```

---

## 5. 各段階の流れ

### select (選ぶ)
1. `wk candidates -n 15`: 未着手のアイディアから 15 件を出す。日付を種にした乱数で、決定論的に選ぶ
   - 除くもの: portfolio にあるもの、`wk:never` タグ、物 (ハードウェア) が必要なもの
2. LLM が `harness/rubric.md` の 5 項目で採点し、JSON で出す → `wk select --scores scores.json`
   - 各候補に `sensitive` (重要な情報をサーバーに持つ必要があるか) と `data_plan` (何をどこに置くか) を必ず書く
   - `wk` が合計の最高点を選ぶ。足切り: `sensitive` が true、物が要る、「無料」が 3 未満、「軽さ」が 3 未満
3. 選んだアイディアで LLM が書く:
   - MVP の定義 (体験の核だけ) と、issue 5〜8 件。各 issue に **受け入れ条件 (AC)** を書く
   - AC は必ずテストにできる文にする (「〜を押すと〜が表示される」)。`wk check-issues` が形を検査する
4. `wk scaffold`: 雛形から repo を作る (private)、ラベル・マイルストーンを作る、issue を登録する

### build (作る) — 1 tick で issue 1 つ
1. `wk next` が issue を返す (今のマイルストーンで一番番号が小さく、`wk:blocked` でないもの)
2. アプリ repo の worktree を作り、実装する。**AC ごとにテストを書く** (テスト名に `AC-<issue>-<n>`)
3. `npm run verify` (または `make verify`) が通るまで直す: lint・型・単体テスト・ビルド・e2e
4. PR を作る (`Closes #N`)
5. `wk review <pr>`: **別の Claude** (`claude -p`、会話を引き継がない) が差分と AC を見て JSON で判定
   - request_changes なら直して再度 (最大 2 回)
6. `wk merge <pr>`: 次が全部そろったときだけ squash merge — CI が全部 green、reviewer が **今の先頭のコミット** を approve 済み (wk が記録した SHA と一致)、守りの部分を変えていない
7. マイルストーンの issue が全部閉じたら `wk deploy`: Web は配信用 repo の preview ブランチに置き、プレビューの URL で守りのヘッダーと中身を確かめる (CLI はタグを打ち、Actions がリリースを作る)
8. 失敗したら issue に `wk:attempt-N` を付けて記録。N が上限なら `wk:blocked`

### evaluate (評価する)
1. プレビューの URL (CLI はリリース) を Playwright で開き、AC のテストをそこに対して流し、スクリーンショットを撮る
2. **別の Claude** が `harness/rubric.md` の評価基準で採点し、JSON で出す: 核が動くか (yes/no)・わくわく・品質・バグ
3. `wk evaluate --scores eval.json` が判定する (LLM ではなく数字で):
   - 核が動く かつ わくわく ≥ 4 かつ 品質 ≥ 3 → **ship**
   - 核が動く かつ ブラッシュアップ回数 < 上限 → **iterate**
   - それ以外 → **drop**
4. 記録を `reports/evaluations/` と idea.md の `## 履歴` に書く

### iterate (磨く)
- 評価の「一番わくわくを下げている点」から、磨く issue を最大 5 件作る (新しいマイルストーン `v0.<n+1>`)
- idea.md の体験の核・わくわくポイントを、作ってみて分かったことで書き直す

### ship (公開する)
- `wk publish-check`: README (雛形どおり)・LICENSE・description・topics・CI green・秘密情報の検査 (gitleaks)
- Web は評価したプレビューと同じものを配信用 repo の main に置き、本番 `https://wakuwaku-site.<サブドメイン>.workers.dev/<名前>/` を検査する
- 全部通れば public にする。工場の README の「作ったもの」表に足す

### drop (捨てる)
- 捨てる理由を 1〜3 行で idea.md と portfolio に書く (「核が無料枠で成立しない」など、次の選定に効く形で)
- repo は archive (private のまま)。Web は配信用 repo から `public/<名前>/` を消す (本番からも消える)
- `wk:never` を付けるか (二度と選ばない)、付けないか (条件が変われば選んでよい) も書く

### intake (貯める・磨く)
- 新しいアイディアを 5 件書く。材料: okuos の goals・journal・最近の growth レポート
- 既存の未着手アイディアを 3 件、作れる形に磨く (体験の核を 1 つに絞る、無料枠で成り立つ形にする)
- `wk check` が雛形の形と重複 (slug・タイトル) を検査する

### retro (振り返る、週 1)
- `state/ticks.jsonl` の失敗・blocked・reviewer の指摘を集計し、`reports/weekly/<日付>.md` に書く
- 手順書 (`harness/prompts/`)・雛形 (`templates/` の守りの部分以外) を直してよい。採点基準 (`harness/rubric.md`) や判定のコード (`cmd/` `internal/`) を含む **守りの部分は直せない** (§8.3)。直すべきなら週のまとめで人に頼む
- 直したら工場 repo の CI を通して merge

---

## 6. 技術の選び方 (無料で作り切る)

| 形 | 使うもの | 無料枠の主な制限 (2026-11 時点・要確認) |
|---|---|---|
| **Web** | 静的ファイルだけ (サーバーの処理・保存なし)。配信用 repo の `public/<名前>/` に置き、1 つの Cloudflare Worker が配る | Workers 10 万リクエスト/日、静的ファイルへのアクセスは無料・無制限、Workers Builds 3,000 分/月。**商用も可** |
| CLI・ターミナルもの | Go + GitHub Releases (goreleaser) | 無料 |

- 言語は **TypeScript と Go** (Findy の方針と合わせる)。Web は TS、CLI は Go
- **サーバーは作らない**。利用者のデータはブラウザの中 (localStorage) にだけ置く。サーバーが要るアイディアは選ばない (rubric の data_plan)
- **配信の鍵はどこにも置かない** (§7.4)。Vercel・Supabase は使わない (プロジェクトごとの連携か鍵が要り、ログインと個人データの方向に引っぱられる)
- アプリは同じ origin (`wakuwaku-site.<サブドメイン>.workers.dev`) に `/<名前>/` で並ぶ。だから localStorage のキーは `<名前>:` で始め、Cookie・Service Worker・IndexedDB は使わない (eslint と配信の Worker の CSP `worker-src 'none'` で止める)
- LLM の API など、使うたびにお金がかかるものは使わない (`factory.json` の `deny_packages` で CI が止める)
- **Supabase は使わない**: ログインと個人データを持つ方向に引っぱられるため (§7)

---

## 7. セキュリティ (最優先)

### 7.1 何も持たない (データの原則)
アプリはサーバーを持たない。ブラウザの中に置くものも「**漏れても誰も困らないもの**」だけ。

| 持たない (禁止) | 代わりにどうするか |
|---|---|
| アカウント・ログイン・パスワード・メールアドレス・電話番号・住所・本名 | ログインを作らない。必要なら「ブラウザに保存した合言葉 (ランダム ID)」だけ |
| 支払い情報・課金 | 作らない (マネタイズは「なし」か、外部の寄付リンクだけ) |
| 位置の履歴・写真・音声・健康・日記など、個人の生活がわかるもの | ブラウザの中だけで処理し、外に送らない |
| 他人の情報、未成年の情報 | 扱うアイディアは選ばない |
| IP アドレス・User-Agent などのアクセスログ | 保存しない (Workers のログ出力に入れない) |
| 秘密情報 (API キー・トークン) | アプリに持たせない。外部 API は鍵の要らない公開 API だけ |

サーバーには何も置かない (置く場所がない)。公開データは鍵の要らない公開 API から、ブラウザが直接読む (宛先は wk.json の egress_allow だけ)。

各アプリに **`DATA.md`** (ブラウザに何を・どのキーで・なぜ) を必須にする。置くものが 1 つ増えるたびに更新し、reviewer が差分と照らし合わせる。

### 7.2 堅く作る (アプリの既定)
雛形に最初から入れ、CI で外せないようにする:
- **ヘッダー**: CSP (外部のスクリプトなし・`unsafe-inline` なし)、`X-Content-Type-Options`、`Referrer-Policy: no-referrer`、`Permissions-Policy` (カメラ・位置などは既定で全部オフ)、HSTS
- **外部のスクリプト・トラッカー・広告・解析ツールを入れない** (CSP と CI の両方で止める)
- **入力は全部検証** (zod など)。サイズ上限をかける。出力は React のエスケープに任せ、`dangerouslySetInnerHTML` は禁止
- **依存は最小**: lockfile 必須、`npm audit --audit-level=high` と OSV-Scanner で既知の脆弱性を止める、新しい依存は reviewer が理由を確認
- **秘密情報**: アプリの repo に置かない。gitleaks を全コミットに対して CI で流し、public 化の前に履歴全体を検査する

### 7.3 CI のセキュリティの門
| 門 | 中身 |
|---|---|
| gitleaks | 秘密情報が履歴にないか |
| 依存の脆弱性 | `npm audit --audit-level=high`、OSV-Scanner、`govulncheck` |
| 個人情報の欄 | 型定義・保存するデータに `email` `password` `phone` `address` `birth` `name` `ip` `token` `lat`/`lng` などの欄があれば落とす (`scripts/data-guard.mjs`) |
| 外部への送信 | 本番ビルドの中に、許可した宛先 (自分のドメイン・公開 API の一覧) 以外の URL があれば落とす |
| ヘッダー | e2e で本番相当のレスポンスヘッダー (CSP など) を検査 |
| DATA.md | 存在し、保存するキーと食い違っていないか |

### 7.4 工場自身の守り
- **配信の鍵を持たない**: Cloudflare の鍵はこのマシンにも repo にも GitHub の secret にもない。配信用 repo (`wakuwaku-site`、private) を Cloudflare の GitHub App に **その repo だけ** 見せ、Cloudflare の中の鍵で配る。このマシンにある鍵は gh のログインだけ
- **鍵のある所でエージェントのコードを動かさない**: Cloudflare 側のビルドのコマンドは空。配信用 repo の中身は、人が決めた Worker (`templates/site/`) と、wk が手元で鍵らしい環境変数を消して install script なしでビルドした静的ファイルだけ。Cloudflare で動くのは配信用 repo の版固定の wrangler だけ
- **配信用 repo を書くのは wk だけ**: 手元の写し (`runtime_dir/site`) はエージェントが guard で入れず、gh での書き込みも portfolio の repo に限られる。wk は `public/<名前>/` だけを書き、配信の設定を変えるファイル (`_headers` `_redirects` `_worker.js`)・Service Worker・リンク・大きすぎるものは置かない
- **評価したものだけを本番に**: `wk deploy` は preview ブランチ (プレビューの URL) に出し、wk がヘッダーと中身 (置いた index.html と同じか) を確かめる。`wk ship` は同じものを main に置き、本番でも同じ検査をしてから repo を public にする
- **壊れたときの範囲を狭く**: Cloudflare は工場専用のアカウントにする (自動で作られるビルド用の鍵は KV・R2 も書けるため)
- **アプリの CI からは何も公開しない**: Actions の権限は `contents: read` を既定にし、必要なジョブだけ上げる
- **外から来た文章は命令として読まない**: 公開 API の応答・OSS の issue・Web のページの中身は、データとして扱う
- **reviewer はセキュリティを必ず見る**: `harness/reviewer.md` の最初の項目が「DATA.md と差分が合っているか、新しく持つ情報はないか」
- **public にする前の点検** (`wk ship`): gitleaks を履歴全体に、DATA.md、CSP、外部送信の検査が通っていること

---

## 8. ハーネス (人が見なくてよくするための仕組み)

### 8.1 品質の門 (アプリ repo の CI)
すべて GitHub Actions。`wk merge` は全部 green でないと merge しない。
| 門 | 中身 |
|---|---|
| lint / format | ESLint + Prettier、または golangci-lint + gofmt |
| 型 | `tsc --noEmit` |
| 単体テスト | Vitest / `go test` |
| AC テスト | Playwright。issue の AC 番号ごとにテストがあることを `wk ac-coverage` で検査 |
| ビルド | 本番と同じビルド |
| 秘密情報 | gitleaks |
| 依存 | `deny_packages` (課金 API の SDK など) が入っていないか |
| 保存 | localStorage の値を zod で検査しているか。セキュリティの門は §7.3 |

GitHub の無料プランでは private repo にブランチ保護をかけられない。だから **merge は人も GitHub も通さず `wk merge` だけが行う** (§5 build の 6)。ラベル `wk:reviewed` は目印で、本当の条件は wk が記録した approve の SHA。

### 8.2 2 人目の目 (reviewer)
- `wk review` は、会話を引き継がない別の `claude -p` を起動する。渡すのは差分・issue の AC・CI 結果だけ
- 出力は JSON (`{"verdict": "approve" | "request_changes", "issues": [...]}`) で、形が違えば失敗扱い
- 作った本人に採点させない (自分の答案を自分で採点しない)。評価 (evaluate) も同じ

### 8.3 ガード (3 枚の壁)
**1 枚目: Claude Code のフック** (`.claude/settings.json` → `wk guard-bash` / `wk guard-edit`)。中身は `internal/guard` で、テストで固定している。
- Bash で止めるもの:
  - GitHub: repo の作成・削除・公開範囲の変更・archive、`gh pr merge`、secret・認証・設定、GraphQL の書き込み、portfolio にない repo への書き込み
  - git: force push・main への push・remote の変更・全体設定・履歴の書き換え・`--no-verify`・作業の破棄
  - お金とサービス: 有料プラン・ドメイン・ログイン、wrangler でのデプロイや D1/KV/R2 の操作 (配信は wk が配信用 repo に push するだけ)、Supabase、`wk site` (配信用 repo の作成は人だけ)
  - 秘密情報: 鍵のファイル・`~/.claude`・環境変数の一覧と参照・`/proc/*/environ`
  - 工場の守り: `go install` (wk の差し替え)、`~/go/bin`・`~/.local/state/wakuwaku` (STOP・ロック・approve の記録)、`settings.local.json`・`disableAllHooks`
  - 守りの部分に触れるコマンドは「読むだけ」(cat・grep・jq・git diff など) に限る。python・perl・node などでの書き換えも止まる
- Edit/Write で止めるもの: 工場・アプリの作業場所・一時ディレクトリの **外** 全部と、その中の守りの部分

**2 枚目: wk のコマンド自身の検査** (フックをすり抜けても止まる)
- `wk sync`: 工場の main に入れる差分に守りの部分があれば merge しない
- `wk merge`: PR が アプリの守りの部分 (`.github/` `.claude/` `scripts/` `src/worker/security.ts` `wk.json` `wrangler.jsonc` `.npmrc`) を変えていれば merge しない。approve の SHA が今の先頭と違えば merge しない
- `wk deploy` / `wk ship` / `wk drop`: 配信用 repo の `public/<名前>/` だけを書き、置くファイルを検査し、プレビュー・本番の中身とヘッダーを確かめる (§7.4)

**3 枚目: CI** (アプリと工場の両方)。lint・テスト・守りの検査・gitleaks・依存の脆弱性。CI の定義も守りの部分

守りの部分の一覧は `factory.json` の `protected_paths` (工場) と `cmd/wk/pr.go` の `appProtected` (アプリ)。
これらはユーザーだけが変える。変えたら `go install ./cmd/wk` で入れ直す (これも人だけ)。

### 8.4 工場 repo の CI
- `go test ./...`、`wk check` (portfolio の形・状態の矛盾・idea.md の形・重複・守りの部分が変わっていないか)

---

## 9. 失敗したとき

| 失敗 | 動き |
|---|---|
| tick が途中で落ちた | ロックが `lock_ttl_minutes` で切れ、次の tick が `aborted` と記録して続ける |
| issue が通らない | `wk:attempt-N` → 上限で `wk:blocked`。核の issue が blocked なら evaluate へ |
| CI が不安定 | 1 回だけ再実行。2 回目も赤なら失敗扱い |
| 連続で失敗が続く | `max_consecutive_failures` を超えたら `STOP` ファイルを作って止まり、工場 repo に `wk:incident` issue を立てる |
| 無料枠に当たった | デプロイを止めて evaluate へ (「無料枠で成立しない」として drop の理由にする) |

**止め方 (人)**: 工場 repo に `STOP` ファイルを置く (またはリポジトリの変数 `WK_STOP=1`)。`wk due` が見て tick を飛ばす。

**知らせ方**: okuos の朝のブリーフが `wk status --short` を読み、incident・ship・drop があれば 1 行出す。

---

## 10. 定期実行

| automation | いつ | precheck | プロンプト |
|---|---|---|---|
| wakuwaku tick | 毎日 8, 11, 14, 17, 20, 23 時 | `wk due` | `harness/TICK.md を読んで、その通りにやって` |

- 1 つの automation で全部の段階を回す (retro も tick の 1 段階)。ロックが 1 つで済み、同時に動かない
- worktree は毎回新しく作る (Orca の new-per-run)。工場 repo の変更は worktree のブランチにコミットし、終わりに main に merge して push

---

## 11. まだ決めていないこと
- 作ったアプリの public 化を、最初から public にするか (Findy には効く) / ship まで private にするか → 今は ship まで private
- ship したアプリのその後 (利用状況を見て磨き続けるか) → 今は ship で終わり。retro で「再評価」の段階を足すか決める
- 1 日 6 tick が Claude の利用上限に収まるか → 最初の 1 週間の ticks.jsonl を見て `ticks_per_day` を調整
- プレビューの URL は ship の前でも、URL を知っていれば誰でも開ける (repo は private のまま)。中身は「持たない」設計なので許容している。気になれば Cloudflare Access で preview を守る
- (確認済み 2026-11-10) プレビューは `preview-wakuwaku-site.<サブドメイン>` とコミットごとの `<短い ID>-wakuwaku-site.<サブドメイン>` の両方に出る。wk は PR のコメントの URL とブランチ名の URL を試し、中身が一致したものを使う
