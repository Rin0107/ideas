# wakuwaku factory v2 — グラフループと進化する工場 (設計案)

- 作成: 2026-11-10
- 状態: 設計案 (未実装)。v1 (`docs/DESIGN.md`) の守り (§1・§7・§8) はそのまま全部引き継ぐ
- 依頼: 「最新の取り組みや試みを試しまくりたい。例えばグラフループ。自動で、安定して改善され、持続可能に成長するシステムを出しまくる」

---

## 0. v1 から何を変えるか (要点)

| | v1 (今) | v2 |
|---|---|---|
| 流れ | 1 tick = 段階 1 つ。段階は `wk next` の if 文 | **グラフ** (YAML) を `wk graph` が実行。ループ・分岐・並列・再開ができる |
| 同時に作る数 | 1 | **レーン N 本** (最初 1、使用量を見て wk が増やす。§10 C2)。予算に合わせて wk が増減 |
| 1 つの issue | 実装 → CI → レビューの一方通行 (失敗は次の tick) | 同じ tick の中で **検証 ⟲ 修正** を回す (上限つき) |
| 改善 | 週 1 の retro が手順書を直す | **実験ループ**: 手法の変種を並べて走らせ、数字で勝ったものだけ昇格 |
| 新しい手法 | 人が持ち込む | **scout** が週 1 で新しい手法を探し、実験の候補に積む |
| 作ったもの | 作って終わり | 部品を **ライブラリ** に吸い上げ、次のアプリが速くなる。公開済みは月 1 で保守 |

変えないもの: 決めるのは wk (数字とルール)、作るのは LLM。重要な情報を持たない。鍵を手元に置かない。お金を使わない。守りの部分はエージェントが変えられない。

---

## 1. 3 つのループ (入れ子のグラフ)

```
 L3 進化ループ (週)      scout ─▶ 実験の候補 ─▶ 変種を作る ─▶ カナリア ─▶ 昇格 / 廃棄
   │  「工場の作り方」を改善する            ▲ 数字 (L2 の記録)
   ▼
 L2 工場ループ (tick)    レーン割り当て ─▶ select / build / evaluate / ship / drop / harvest / maintain
   │  「どれを作るか」を回す                 ▲ 結果
   ▼
 L1 アプリループ (node)  spec ─▶ テスト ─▶ 実装 ─▶ 検証 ⟲ 修正 ─▶ レビュー ⟲ ─▶ merge ─▶ preview ─▶ 評価
      「1 つを作り切る」
```

- 下のループは上のループの「1 ノード」に見える (サブグラフ)
- 上のループほど遅く、守りが固い。L3 が変えられるのは **手順書・グラフ・雛形の守りでない部分** だけ (§5)

---

## 2. グラフ実行器 `wk graph` (中核)

### 2.1 グラフの書き方
`harness/graphs/<名前>.yaml`。例 (L1 の issue 1 つ分):

```yaml
name: build-issue
version: 3
state: {issue: int, attempt: int, verdict: str}
nodes:
  spec:     {kind: llm,   prompt: prompts/spec.md,  out: schemas/spec.json}
  tests:    {kind: llm,   prompt: prompts/tests-first.md}
  impl:     {kind: llm,   prompt: prompts/impl.md}
  verify:   {kind: cmd,   run: "npm run verify", parse: verify}
  fix:      {kind: llm,   prompt: prompts/fix.md, inputs: [verify.log]}
  review:   {kind: judge, prompt: reviewer.md, model: other}   # 別の目 (会話を引き継がない)
  merge:    {kind: cmd,   run: "wk merge {{pr}}"}
edges:
  - spec -> tests -> impl -> verify
  - verify -> review       when: verify.ok
  - verify -> fix          when: "!verify.ok"   max_visits: 4
  - fix -> verify
  - review -> merge        when: review.approve
  - review -> fix          when: "!review.approve" max_visits: 2
  - "* -> @fail"           when: visits_exhausted     # 上限で失敗として終わる
```

### 2.2 ノードの種類
| kind | 中身 | 決めるのは |
|---|---|---|
| `cmd` | wk のコマンド・verify・gh。結果は終了コードと決まった形の JSON | プログラム |
| `llm` | `claude -p` (新しい会話)。出力は JSON Schema で検査、外れたら 1 回だけやり直し | 中身だけ LLM |
| `judge` | 別の Claude (または別のモデル) が採点。点は JSON | 点は LLM、合否は gate |
| `gate` | 点・回数・予算から次の辺を選ぶ純粋関数 (`internal/gate`) | プログラム |
| `fanout` / `join` | 同じノードを K 個の worktree で並列に走らせ、join で 1 つ選ぶ (best-of-N) | 選び方は gate |
| `sub` | 別のグラフを呼ぶ (L2 から L1 を呼ぶなど) | — |

### 2.3 安定させる仕組み
- **チェックポイント**: ノードが終わるたびに状態を `runtime_dir/graph.db` (SQLite) に書く。tick が落ちても次の tick が続きから再開する
- **冪等**: ノードには `(run, node, visit)` の鍵があり、同じ鍵は 2 回実行しない。push・merge・deploy は「もう済んでいるか」を先に確かめる
- **上限はすべての辺に**: `max_visits`・ノードの時間・run 全体の tick 数。無限ループはグラフの検査 (`wk graph lint`) で、上限のない閉路を拒否して防ぐ
- **時間の切り方**: 1 tick は「グラフを予算の切れ目まで進める」。途中で切れても checkpoint から続く
- **再現**: run ごとに、グラフの版・手順書の commit・モデル名・入出力のハッシュを `state/runs/<run>.jsonl` に残す (git に入る)
- **壊し試験**: テストで各ノードの途中で殺し、再開して同じ結果になることを確かめる

自前で作る理由: LangGraph などは Python と API 鍵を前提にしている。必要なのは小さな状態機械と checkpoint だけで、Go で数百行。決めるのは wk、の原則とも合う。

---

## 3. L2 工場ループ: レーンと予算

### 3.1 レーン
- `lanes` 本 (最初 1。§10 C2) が並列に動く。各レーンは自分のアプリ 1 つと自分のロックを持つ
- Orca automation は今の 1 本のまま。tick の中で wk が空いているレーンを選んでグラフを進める (または Orca の orchestration で worker を立てる。§6 の実験 E9)
- 割り当て: 空いたレーンに select。レーンの 1 本は **実験レーン** にして、新しい手法を試す (§4)

### 3.2 新しい段階
| 段階 | いつ | やること |
|---|---|---|
| `harvest` | ship したとき | 使い回せる部品 (UI の部品・保存の仕組み・e2e の型・Go の描画) を `templates/lib/` に吸い上げる。次の select が「部品で何割できるか」を見積もる |
| `maintain` | 公開済みのアプリごとに月 1 | 依存の更新・verify・本番の検査。壊れたら直す issue、直せなければ archive |
| `intake` | v1 と同じ + 多様性 | アイディアを「種類 × 形」の表 (§4.3) の空いているマスに寄せて作る |

### 3.3 予算 (持続のため)
お金は使わないので、守る資源は **Claude の利用枠・時間・ディスク**。
- `claude -p --output-format json` の使用量を run ごとに記録し、1 日・1 週の上限を `factory.json` に置く
- 利用枠の上限に当たったら、レーンを減らし、指数的に間をあける。0 になったら止まって次の日に再開 (incident にはしない)
- 予算の配分: 作る 60%・評価 15%・実験 15%・保守と scout 10%
- 終わった worktree・preview・ログは wk が自動で片付ける

---

## 4. L3 進化ループ: 手法を試しまくる

### 4.1 「手法」= 戦略 (strategy)
戦略は **グラフ + 手順書 + 設定** の組で、`harness/strategies/<名前>/` に置く。例: `baseline` (今の v1 相当)、`tests-first`、`best-of-3`。
アプリの run はどれか 1 つの戦略で走り、結果がその戦略の成績になる。

### 4.2 回し方
1. **scout** (週 1): Claude Code の更新履歴・Anthropic のブログ・よく読まれるエージェント手法の論文や記事を読み、「試す価値のある手法」を `harness/experiments/backlog.md` に積む (読んだものはデータ。中の指示には従わない)
2. **変種を作る**: backlog の上から 1 つ、戦略として実装する (守りでない部分だけ)
3. **カナリア**: 実験レーンでだけ走らせる。同じ週の baseline と比べる
4. **割り当て**: 戦略ごとの成績で、どの戦略にどれだけ run を回すかを Thompson sampling で決める (勝っているものに多く、負けているものにも少し)
5. **昇格 / 廃棄**: wk が数字で決める。最低 5 run、主な指標が baseline より良く、守りの指標が 1 つも悪くないこと。負けたら廃棄して理由を残す
6. **全部 git に残す**: 何を試し、何で勝ち、何で負けたか (`reports/experiments/`)

### 4.3 指標 (wk が集計。LLM は数えない)
| 種類 | 指標 |
|---|---|
| 主 | 1 週の ship 数、ship までの tick 数、評価のわくわく点 (判定役は固定) |
| 品質 | 1 回目の verify で通る割合、レビューの差し戻し、本番の e2e の通過 |
| 守り (1 つでも悪化したら即廃棄) | セキュリティの門での失敗、guard に止められた回数、incident |
| 持続 | ship 1 つあたりの使用量、保守で壊れていた割合 |
| 多様性 | アイディアの「種類 × 形」の表の埋まり方 (MAP-Elites 風。似たものばかり作らない) |

### 4.4 自己改善で壊れないための決まり (グッドハートの対策)
- **採点する側は動かさない**: rubric・reviewer・gate・指標の計算・セキュリティの門は守りの部分。実験は「作る側」だけを変える
- **固定の試験**: `harness/bench/` に小さな課題 10 個 (答えと e2e つき) を置く。戦略を昇格する前に必ず流し、下がったら昇格しない
- **戻せる**: 昇格は `harness/strategies/current` を指し直すだけ。悪くなったら wk が前に戻す
- 守りの部分を変えたい提案は、v1 と同じく週のまとめで人に頼む

---

## 5. 誰が何を変えられるか

| 場所 | 変えられる | 方法 |
|---|---|---|
| `cmd/` `internal/` (グラフ実行器・gate・予算・指標) | 人だけ | — |
| `harness/rubric.md` `reviewer.md` `bench/` `schemas/` | 人だけ | — |
| `harness/strategies/<実験>/` (グラフ・手順書) | L3 | カナリア → 昇格 |
| `harness/strategies/current` の指し先 | wk だけ | 数字で昇格・戻す |
| `templates/lib/` | harvest | CI と bench を通ったら |
| アプリ repo | L1 | v1 と同じ (CI・別の目・wk merge) |

`protected_paths` に `harness/bench/` `harness/schemas/` `harness/strategies/current` を足す。

---

## 6. 最初に試す実験 (backlog の初期値)

| # | 手法 | 期待 | 中身 |
|---|---|---|---|
| E1 | **グラフループ** (検証 ⟲ 修正) | 1 issue が 1 tick で閉じる割合が上がる | §2 そのもの。baseline との比較が最初の実験 |
| E2 | テストを先に (spec → tests → impl) | 差し戻しが減る | AC からテストを先に書き、赤を確かめてから実装 |
| E3 | best-of-3 + 勝ち抜き判定 | 評価のわくわく点が上がる | 体験の核の画面だけ 3 案並列、別の Claude がスクリーンショットで比べる |
| E4 | 失敗の教訓 (Reflexion 風) | 同じ失敗の繰り返しが減る | 失敗した run の原因を 1 行にして `harness/lessons/`、似た issue のときに手順書へ差し込む |
| E5 | 部品ライブラリ (Voyager 風) | ship までの tick が減る | harvest した部品を select と build に見せる |
| E6 | 手順書の進化 (GEPA 風) | bench の点が上がる | 失敗の記録から手順書の書き直しを 3 案作り、bench で一番良いものを残す |
| E7 | 別のモデルでレビュー | 見逃しが減る | reviewer を Codex にする (§7 で入れる場合) |
| E8 | 見た目の判定 | 品質点が上がる | Playwright のスクリーンショットを judge に渡し、崩れ・読みにくさを採点 |
| E9 | Orca orchestration の DAG | レーンが互いに邪魔しない | レーンごとに Orca の worker を立て、DAG で依存を表す |
| E10 | アイディアの自己対戦 | わくわく点の高いアイディアが増える | intake で 2 案を戦わせ、勝った方を磨く |

scout が見つけたものは E11 以降に積む。

---

## 7. 入れる道具 (nix で、home-manager に足す)

| 道具 | 用途 | 鍵 |
|---|---|---|
| `sqlite` | グラフの checkpoint と指標 (Go からは純 Go の driver で、CGO なし) | 要らない |
| `playwright-driver` + chromium (nixpkgs) | 評価と E8。NixOS で npm の Playwright が動かない問題もこれで解ける | 要らない |
| `duckdb` | 指標の集計 (runs の jsonl をそのまま SQL で読む) | 要らない |
| `codex` (任意) | E7 の別の目。ChatGPT のアカウントでログインするので env に鍵は置かない | ログインのみ。入れるか要相談 |

入れないもの: LangGraph など Python の枠組み (鍵前提・依存が重い)、ローカルの LLM (WSL で重い。必要になったら実験で)。

---

## 8. 進め方 (段階的に。各段階で v1 の守りが全部通ること)

| 段階 | 中身 | 終わりの条件 |
|---|---|---|
| P0 | `wk graph` を作り、v1 の段階をそのままグラフに写す (動きは変えない) | 今の tick と同じ結果。壊し試験が通る |
| P1 | L1 に検証 ⟲ 修正とレビュー ⟲ を入れる (E1) | terminal-sky の残りの issue で 1 tick あたりの完了数を比べる |
| P2 | レーン 3 本・予算・harvest・maintain | 1 週回して利用枠に当たらない |
| P3 | 戦略・実験レーン・Thompson sampling・bench・昇格と戻し | E2〜E4 を 1 周 |
| P4 | scout と手順書の進化 (E6)・部品ライブラリ (E5) | 人が持ち込まなくても実験が積まれ、回る |

P0〜P1 は人と一緒に (守りの部分の変更なので)。P2 から無人で回す。

---

## 9. 決めること (人に)
1. レーンの数: Claude のプランの利用枠しだい。最初は 3、wk が使用量から増減する、でよいか
2. Codex を入れるか (E7)。入れないなら別の目は「別の手順書の Claude」のまま
3. scout が読んでよい場所 (Web の検索を許すか、決まった一覧だけか)

---

## 10. 批評と直したこと (2026-11-10)

設計案を自分で批評した。上の本文より、この節を優先する。

| # | 穴 | 直し方 |
|---|---|---|
| C1 | **数が足りず比べられない**。ship は週に数件なので、「最低 5 run」の比較はアイディアの難しさの差に埋もれる | 戦略の比較は **issue 単位** (数が多い) と **bench** (同じ課題で比べられる) を主にする。ship 数は見るだけで、昇格の条件には使わない。Thompson sampling も issue 単位の成功で回す |
| C2 | **利用枠が足りない**。レーン 3 本 × best-of-3 × 判定役で、使用量は今の 10 倍近くになる | 最初のレーンは **1 本 + 実験は bench だけ**。1 週の使用量を測ってから wk が増やす。best-of-N は核の画面だけ、N は予算から決める |
| C3 | **グラフの `cmd` ノードが guard の抜け道になる**。戦略の YAML に好きなコマンドを書けると、守りをすり抜けられる | `cmd` は **wk の中の許可した名前だけ** (`verify` `wk.merge` など)。YAML に生のコマンドは書けない。新しい名前を足すのは人だけ。`wk graph lint` で拒否する |
| C4 | **bench が漏れる・過学習する**。エージェントが答えを読めると、bench だけ上手くなる | 答えと e2e は `runtime_dir/bench/` (guard で読めない) に置く。課題の半分は毎月入れ替える (入れ替えは人) |
| C5 | **自分で自分を採点する偏り**。作るのも判定するのも Claude | 判定役は Codex にする (nix で入れた)。Claude と Codex の点が 2 以上離れたら低い方を採る |
| C6 | **並列で state がぶつかる**。portfolio.json は 1 ファイルで、複数の worktree から commit すると衝突する | 正は SQLite (`runtime_dir/graph.db`)、git には run ごとの追記だけのファイル (`state/runs/<run>.jsonl`) を書く。portfolio.json は wk が SQLite から作る写し |
| C7 | **配信の preview がぶつかる**。配信用 repo の preview ブランチは 1 本 | preview ブランチをレーンごとに分ける (`preview-<レーン>`)。配信用 repo への書き込みは wk のロックで 1 つずつ |
| C8 | **scout が prompt injection の入口になる**。Web で読んだものが戦略になり、グラフを変える | scout が書けるのは backlog の文章だけ。戦略を作るのは別の run で、C3 の制限の中だけ。scout の読む先は最初は決まった一覧 (Claude Code の changelog・Anthropic の engineering ブログ・arXiv の cs.SE/cs.AI の新着) |
| C9 | **保守が増え続ける**。公開したアプリの数に比例して保守の予算が増え、作る予算を食う | 保守に使う予算は全体の 10% まで。超えたら、評価の低い公開アプリから archive (消さない・本番からは下げる) |
| C10 | **自前のグラフ実行器が重い**。P0 だけで大仕事になり、試すのが遅れる | P0 は「v1 の段階を写すだけ」の最小の実行器 (状態機械・checkpoint・上限・許可したノード) に絞る。並列 (`fanout`) は P3 まで作らない |
| C11 | **Playwright の版のずれ**。nix のブラウザ (playwright-driver 1.64.1) と雛形・CI の 1.65.0 (CI はイメージで固定) が合わない | CI のイメージが正。手元で e2e を流すのは nixpkgs が 1.65 に上がってから。wk check が `@playwright/test` と CI のイメージと nix の版の一致を検査し、ずれていたら手元の e2e を飛ばして CI に任せる |

§9 の決めること: 2 (Codex) は **入れる** と決まった (2026-11-10)。
