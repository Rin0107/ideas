# TESTING — 正しさの決まり

守りの部分 (エージェントは変えられない)。工場 (wk) と、工場が作るすべてのアプリ (雛形 `templates/go-cli`・`templates/web`) に効く。

## 何を保証できて、何を保証できないか

「99.999999% 壊れない」は、テストでは**保証も測定もできない**。テストは「試した範囲で壊れていない」ことしか示せず、
形式手法が証明するのは「仕様 (モデル) が正しい」ことで、コードそのものではない。
そこで、目標を **測れて、機械が守らせられる** 形に置き換える:

| 目標 | どう確かめるか | 守らせ方 |
|---|---|---|
| 仕様が明確で正確 | 状態の流れを TLA+ で書き、TLC が全状態を調べる。算術・条件の性質を Z3 で証明する | CI の formal。落ちたら merge しない |
| 実装が仕様どおり | 仕様の不変条件ごとに、同じ性質を実装に対して確かめるテスト (`TestSpec_<名前>` / `"Spec_<名前>: …"`) を PBT か全件ループで書く | `scripts/formal.sh` が、cfg の INVARIANT ごとにテストがあるかを検査 |
| テストが全部の道を通る | カバレッジ 100% (Go: S と M を合わせて文単位、main も含む。TS: 行・分岐・関数・文) | 1 文でも通らなければ落ちる (`scripts/cover-check.sh` は丸めずに数える) |
| テストが本当に壊れを見つける | ミューテーション: コードを少し壊し、テストが気づくか | efficacy (score) 95% 以上・mutant coverage 100%。変えたコードだけを毎回 |
| 想定外の入力で落ちない | fuzzing (Go の fuzz・fast-check) | CI の fuzz が時間を区切って毎回探す。シードは毎回の S のテストでも流れる |
| 速い | どの CI ジョブも 5 分で打ち切り | `timeout-minutes: 5`。超えたら失敗 = merge できない |

95% にしている理由: 中身の変わらない変異 (等価ミュータント。例: `?? "0"` と `?? ""` が同じ数になる) は、テストでは殺せない。
100% を必須にすると、正しいコードでも永久に merge できなくなる。生き残った変異は、テストが足りない印か、コードを等価でない形に書き直す合図として扱う。

## テストの種類と大きさ (Google の S / M / L)

| 大きさ | 決まり | 中身 | どこで |
|---|---|---|---|
| **S** | 1 プロセス・ネットワークなし・速い | 単体・**境界値** (端とその 1 つ外)・**全件ループ** (小さな範囲は全部の組み合わせ)・**PBT** (Go: rapid、TS: fast-check)・fuzz のシード・`TestSpec_` | `make test` / `npm test`。毎回 |
| **M** | 手元で完結する複数プロセス・ディスク | Go: 実行ファイルを作って動かす (`//go:build medium`、`TestM_`)。web: 手元の Worker をブラウザで e2e | CI の verify / e2e。毎回 |
| **L** | 本物の外部 | web: 本物のプレビューの URL をブラウザで e2e (`preview-e2e.yml`、wk deploy が起動して結果を待つ)。go-cli: Releases から落とした実行ファイルを動かす (`release.yml`) | デプロイ・リリースのたび |

形式手法 (TLA+・Z3) は S と並んで毎回流れる (CI の formal)。

## 道具 (版を固定)

| 道具 | 版 | 入れ方 |
|---|---|---|
| TLA+ (tla2tools.jar) | 1.7.4 | CI が sha256 を確かめて落とす |
| Z3 | 5.1.0 | 同上 |
| rapid (Go の PBT) | v1.3.0 | go.mod |
| gremlins (Go のミューテーション) | v0.6.0 | CI が `go install` |
| fast-check | 4.10.2 | package.json (固定) |
| Stryker (TS のミューテーション) | 10.1.1 | package.json (固定) |
| vitest のカバレッジ (v8) | 5.1.0 | package.json (固定、vitest と同じ版) |

## 手元の機械を守る (並列の数とメモリ・CPU の上限)

2026-11-10 に、手元でメモリが尽きて PC ごと落ちた。どの道具も既定では「全コア・メモリの上限なし」で動く
(gremlins は `runtime.NumCPU()` の数だけ go test を同時に動かす。TLC は `-workers auto`、Java のヒープは物理メモリの 1/4)。
そこで、重いものは**並列の数とメモリ・CPU の上限を全部はっきり決めて**動かす。

| 段 | 何をするか | どこで |
|---|---|---|
| 1 本ごとの上限 | `scripts/limit.sh <メモリ> <CPU 数> <コマンド>`。手元では systemd の cgroup (scope) に入れ、超えたら**そのコマンドだけ**が止まる (swap も使わない)。CI (`CI=true`) ではそのまま動かす | Makefile・package.json・fuzz.sh・formal.sh |
| 並列の数 | 手元 2・CI 4 (`JOBS`)。`GOMAXPROCS`・`go test -p`・fuzz の `-parallel`・gremlins の `--workers`・TLC の `-workers`・vitest の `maxWorkers`・Stryker の `concurrency`・Playwright の `workers` | 同上 |
| 道具ごとのメモリ | Go: `GOMEMLIMIT=1GiB` (柔らかい上限)。TLC: `-Xmx768m`。Z3: `-memory:1024`。Node: `--max-old-space-size=1536` | 同上・`.claude/settings.json` の env |
| エージェントの既定 | `.claude/settings.json` の env (GOMAXPROCS=2 など)。エージェントが手で打つコマンドにも効く | 工場と雛形 |
| 始める前の門 | `wk due` が使えるメモリ・メモリ待ち (PSI)・CPU の load・cgroup の残りを見て、足りなければ tick を始めない (`internal/room`) | 工場 |
| 機械全体 | WSL 6GB・6 コア、Orca の端末全体の cgroup 4G、earlyoom | nix-config (この repo の外) |

| 重いもの | 並列 | メモリの上限 (1 本) |
|---|---|---|
| go vet・go test・カバレッジ・build・govulncheck | 2 | 2G |
| fuzz (1 つの Fuzz ずつ) | 2 | 2G |
| gremlins (ミューテーション) | 2 | 3G |
| TLC (TLA+) | 2 | ヒープ 768m・cgroup 1536M |
| Z3 | 1 | 1024MB・cgroup 1536M |
| vitest・tsc・eslint・vite build | 2 | 2G |
| Stryker | 2 | 3G |
| Playwright (e2e) | 2 | 3G |

上限を外して調べたいときは `LIMIT_OFF=1`。上限そのもの (scripts/・Makefile・設定) は守りの部分で、エージェントは変えられない。

## 書き方の決まり

- **仕様が先**。振る舞いを変える PR は、spec・実装・`TestSpec_` を同じ PR で変える
- **境界値**: 範囲のある入力は、端 (最小・最大) とその 1 つ外を必ず。ミューテーションの境界の変異 (`<` → `<=`) はこれでしか殺せない
- **全件ループ**: 入力の組み合わせが数千以下なら全部を回し、回った件数も確かめる (漏れを防ぐ)
- **PBT**: 実装と、仕様を写した小さなモデルを突き合わせる (モデルベース)。往復 (parse → 文字列) も
- **fuzz**: 外から入力を受け取る関数ごとに 1 つ。「落ちない」だけでなく「受け付けたものは正しい形」まで確かめる
- **must()**: 起きないはずのエラー (自分の型の JSON 化など) は、`must` で panic させてよい。panic する道も 1 回テストする
- テストの中の `must()` も同じ (テスト用の小さな関数)

## 工場自身

- `spec/Factory.tla`: アプリ 1 つの一生 (選定 → 作る → 評価 → 磨く / 公開 / 捨てる)。不変条件 5 つと、
  「どのアプリもいつか必ず終わって次に移る」(生きていること) を TLC が調べる
- `spec/verdict.smt2`: 評価の結論の性質 (iterate は回数が残っているときだけ・核が動かなければ ship しない など) を Z3 で証明
- 雛形そのものも CI で検査する (`template-go`・`template-web`・`template-web-e2e`): 雛形からアプリを作り、アプリの CI と同じ検査を流す
- カバレッジ: 工場全体 (`cmd/wk` と `internal/`、S と M を合わせて、実行ファイルの main も含む) で 100% を CI で必須
- 外の道具の代わり: gh は `internal/fakegh` (決まりで答える・最初の N 回だけ・途中でシェルを動かす)、git は本物 (https://github.com/ を手元の bare repo に向ける)、
  Cloudflare は「push された中身を返す手元の TLS サーバー」、npm と claude は偽物の実行ファイル。git の失敗は「合った呼び出しだけ失敗させる」包みで起こす
- ミューテーション: `internal/` と `cmd/wk` を別のジョブで (変えたコードだけ)
