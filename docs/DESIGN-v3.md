# wakuwaku factory v3 — 継ぎ目を減らし、トークンで回す

- 作成: 2026-11-15
- 状態: 実装済み。v1 (`docs/DESIGN.md`) の守り (§1 守ること・§7 セキュリティ・§8 ガード・§9 自分で直す) はそのまま全部引き継ぐ
- 依頼: 「wk tick、一回当たりの進捗が悪い」「もっととにかく本質的に wk の仕組みを考え直して」

---

## 0. なぜ作り直したか (2026-11-10〜11-15 の数字)

| | |
|---|---|
| 予定した実行 | 82 回 |
| Claude が動いた | 29 回 (53 回は始める前の門で飛ばされた) |
| アプリが前に進んだ | 3 回 (select 1・build の merge 2) |
| 同じ evaluate のくり返し | 約 10 回 (sync が落ちて結果が main に入らず、毎回やり直した) |
| 1 回の実行で進んだ段 | ほとんど 1 段 (3 段まで進めるのに、1 段目のあとの `wk sync` で止まった) |
| できたもの | terminal-sky 1 つ (2,093 行、わくわく 3) |
| 工場のコード | 制御 4,540 行 + テスト 6,069 行 |
| 最後 | 週の利用上限に当たって止まった |

**原因は「1 tick = 1 段」にしたこと**。段ごとに、起動・手順書を読む・ロック・子のエージェントに渡す・記録・commit・sync という固定の手間がかかり、それが中身より大きかった。
失敗のほとんども、この継ぎ目で起きた (sync・ロック・state)。

1. **状態を 4 か所から書いていた** — tick の worktree・手元の main (wk deploy がアプリから呼ばれると書いた)・GitHub のラベル・runtime。main が汚れる・記録が消える・同じ評価をくり返す、はここから
2. **わくわくを最後まで測らなかった** — 何日もかけて MVP を作り切ってから採点した。「見どころが弱い」は 1 時間の試作でも分かった
3. **守りの重さが中身に見合っていなかった** — おもちゃの CLI に TLA+・ミューテーション・カバレッジ 100%・4 ロールのレビュー。作るより、関門を通すのにトークンを使った
4. **予算を tick の数で数えていた** — 本当の制約は Claude の利用上限 (トークン)。数えていなかったので、上限に当たるまで分からなかった

## 1. 形

```
 Orca automation (1 時間おき。動いてよい時は wk が決める)
   │ wk due: STOP・run_hours・1 日のトークンの予算・機械の余裕・ロック
   ▼
 1 つの Claude (harness/TICK.md) が、止まるまで段を自分でやる
   wk next → harness/prompts/<段>.md → wk done → wk next → …
   例: select → build (MVP を 1 つの PR で) → evaluate → iterate の計画 / drop
   │
   ├─ 状態・記録: wk が自分の clone に書いて、コマンドごとに push (§2)
   └─ アプリ: 1 版 = 1 PR (§3)
```

v2 との違い:

| | v2 | v3 |
|---|---|---|
| 1 回の実行 | 1〜3 段 (実際は 1 段) | 止まるまで (`steps_per_run` 4、予算と時間の内) |
| 段をやる人 | leader が engineer・designer・sre のサブエージェントに渡す | 1 つの Claude が自分でやる (評価の採点だけ別の目) |
| build | 1 issue = 1 PR = 1 tick | 1 版 (マイルストーンの issue 全部) = 1 PR |
| PR のレビュー | sre・engineer・designer (+guardian) | sre (+guardian)。画面は評価で見る |
| 評価 | designer と sre の 2 つ | 1 つのサブエージェントが全部 |
| 状態を書く場所 | worktree → commit → `wk sync` で手元の main へ | wk の clone (`state_checkout`)。コマンドごとに push |
| 磨く条件 | 核が動けば磨く | わくわく 3 以上のときだけ。届かなければすぐ次へ |
| 関門 | いつも全部 | 試作は軽く。重いもの (100%・mutation・formal・fuzz) は公開の前 |
| 予算 | 1 日 12 tick | 1 日 200 万 (トークン + キャッシュの読み出しの 1/10)、1 時・4 時に始める |
| 見回り・仕分け | 3 日おき | 止めた (`patrol_every_days: 0`) |

## 2. 状態を書くのは wk だけ (`state_checkout`)

- wk は `~/.local/state/wakuwaku/factory` に工場の clone を持つ (なければ作る)。guard がこの場所をエージェントに触らせない
- 状態を使うコマンドは、始めに clone を origin の main に合わせ (`pull --rebase`)、終わりに変更を commit して push する。成功でも失敗でも (記録を落とさない)。push に失敗したら、次のコマンドがもう一度試す
- tick の worktree と手元の main (`~/src/okuos/repos/ideas`) には、state を書かない。だから「main が汚れていて sync できない」が起きない
- `wk next` は、前の段の記録が読めるように、tick の worktree を origin の main に進める (worktree に自分の変更があれば触らない)
- 手順書・idea.md・雛形の変更 (worktree で作る) は、`wk sync` が worktree のブランチを clone に取り込んで push する
- idea.md と INDEX.md は、両方の側が末尾に行を足すので union merge (`.gitattributes`)
- 例外: `check` (CI が worktree を検査する)・`sync`・`guard-*` (毎回の道具の呼び出しで動くので速くする) は今の worktree を見る
- 実装: `cmd/wk/v3.go` の `openState`・`settleState`・`finishState`・`refreshWorktree`、`cmd/wk/main.go` の `run`

## 3. 1 回の実行で 1 版を作り切る

- **select**: MVP は体験の核だけの試作。issue は 1〜3 件 (`max_mvp_issues`)、全部を 1 回の実行 (1 時間ほど) で作れる大きさ。デザインシステム・運用手順の文書は、伸びると分かってから
- **build**: `wk next` が今のマイルストーンの issue を全部返す (`issues`)。1 つのブランチ・1 つの PR (`Closes` を並べる)・1 回のレビュー・1 回の CI で作り切り、出す。作り切れなかった issue だけ `wk attempt`。直す issue (`wk:fix`) はそれだけで解く
- **evaluate**: 1 つのサブエージェントが触って全部を採点する。結論は wk (`internal/verdict`)
- **iterate**: わくわくが `iterate_min_wakuwaku` (3) 以上のときだけ。2 なら磨かずに drop して次のアイディアへ
- 時間切れ: MVP は build 3 回 (`max_ticks_mvp`)、1 回の磨きは 2 回 (`max_ticks_iteration`)
- ロック: 1 回の実行が長くなるので 240 分 (`lock_ttl_minutes`)

## 4. 予算はトークン (`max_tokens_per_day`・`run_hours`)

- wk は Claude Code の会話の記録 (`~/.claude/projects/*wakuwaku*/**/*.jsonl`) と、`claude -p` の記録 (`<runtime_dir>/usage-claude-p.jsonl`、wk が書く) から、使った量を数える (`internal/usage`)
- 数えるのは「入力 + キャッシュ作成 + 出力 + キャッシュの読み出しの 1/10」(`Usage.Weighted`)。キャッシュの読み出しは安いが、量がけた違いに多い (2026-11-12 は 5,700 万)
- 今日の量が `max_tokens_per_day` (200 万) に達したら、`wk due` は始めず、`wk next` は idle を返す。v2 の 11-14 (1 日 160 万で、進んだのは 2 段) と同じくらいの量で、どれだけ進むかを見る
- 始めてよいのは `run_hours` (1 時と 4 時) だけ。人が PC を使う時間 (ゲーム・作業) と分け、人の作業の利用上限も残す
- `wk done` は段ごとの量を `state/ticks.jsonl` に書く (`tokens`・`cache_read`)
- `wk usage` は日ごとの量と、アプリごとの量・**わくわく 1 点あたりの量** を出す。retro はこの数字で工場を直す

## 5. 関門は段階に合わせる

| 段階 | 関門 |
|---|---|
| 試作のあいだ (MVP・磨き) | lint・型・AC のテスト (ac-coverage)・ビルド・守りの検査 (deps・data・egress・gitleaks・npm audit・OSV・govulncheck)・sre のレビュー |
| 公開の前 (ship) | 上に加えて、カバレッジ 100% (`make verify-ship` / `npm run verify-ship`)・CI の fuzz・mutation・formal (`workflow_dispatch` で手で起動) |

- **セキュリティの決まりは、どの段階でも全部かける** (重要な情報を持たない・鍵を置かない・お金を使わない)。軽くしたのは「正しさの証明」の重さだけ
- 雛形: `templates/*/.github/workflows/ci.yml` の fuzz・mutation・formal は `workflow_dispatch` のときだけ動く。`templates/go-cli/Makefile` の `verify` からカバレッジ 100% を外し、`verify-ship` に移した。`templates/web/vitest.config.ts` の 100% は `WK_SHIP=1` のときだけ
- 工場そのもの (wk) の関門は変えない (カバレッジ 100%・ミューテーション)。wk は守りの中心なので

## 6. 変えないもの

決めるのは wk (数字とルール)、作るのは LLM。重要な情報を持たない。鍵を手元に置かない。お金を使わない。
守りの部分はエージェントが変えられない。人に頼むのは許可だけ (`wk:human` → メール)。STOP と連続失敗での停止。

## 7. 次に見ること

- 1 回の実行で何段進むか (`state/ticks.jsonl` の時刻)。1 つの版が 1 回で作り切れるか
- わくわく 1 点あたりの量 (`wk usage`)。v3 の最初のアプリと terminal-sky を比べる
- 試作を軽くしたことで、公開の前の重い関門が通らなくなっていないか (ship での retry の数)
- 既存の terminal-sky は古い雛形から作ったので、重い関門 (mutation・formal・100%) はもともと入っていない。そのまま v3 の流れで磨く
- v3.1: 2 本が同時に動いたときの、メモリ (room で止まった回数)・状態のロックの待ち・1 日の量。アプリが何件同時に前へ進むか

## 8. 同時に進める (v3.1、2026-11-15)

依頼: 「もっと頻度高く、もっと同時にいろんなプロダクトの成果だして」。1 つずつ作り切るのを待つと、1 日に前へ進むアプリは 1 つだけだった。

| | v3 | v3.1 |
|---|---|---|
| 作りかけ | 1 件 (WIP 1) | `max_wip` 3 件 |
| 同時に動く実行 | 1 本 (ロック 1 つ) | `max_parallel` 2 本 (実行ごとのロック) |
| 始める時 | 1 時・4 時だけ | いつでも (`run_hours` 空)。Orca は 30 分おきに起こす |
| 1 日の予算 | 200 万 | 500 万 |

- **実行ごとのロック**: `<runtime>/tick-<worktree の名前>.lock` に、進めている段・repo・アイディアを書く (claim)。`wk next` はほかの実行が持つアプリを選ばない。repair・retro・intake・select は全体で 1 つずつ (`next.Global`)。select で決まったアイディアは、その場でロックに書き足す (`bindCurrent`)
- **今の段の記録も実行ごと** (`current-<名前>.json`)。`wk` のコマンドは、tick の中なら `wk next` が渡したアプリ、アプリの repo の中ならその repo を扱う (`env.project`)
- **状態のロック** (`<runtime>/state.lock`、flock): clone に触るコマンドは同時に 1 つだけ。CI やビルドを長く待つ `review`・`merge`・`deploy` (と `due`・`status` など) は読む間だけ持ち、deploy は書くときだけ取り直す (`env.update`)。落ちたプロセスの flock は OS が外す
- **続けて同じアプリ**: 実行は前の段のアプリを先に選ぶ (`Snapshot.Prefer`)。読んだものを使い回せる
- 同時に 2 本までにしたのは機械のため。WSL は 6GB で、重いコマンド (ビルド・テスト) の枠は 4G・1 本 2G (`internal/room`)。`wk due` と `wk next` は始める前に毎回、空きを見る
- 落ちた実行のロックは TTL (240 分) で切れ、次の `wk next` が aborted と記録して外す

## 9. 利用上限で止める (v3.2、2026-11-15)
依頼: 「今日の分も全然余ってるように見える。5h, 7d 制限を毎回確認して、常にいい感じに余るようにして」。1 日 500 万の予算で止まった 21 時、本当の利用上限にはまだ余裕があった。1 日の量は本当の制約 (Claude の 5 時間・週の利用上限) の代わりでしかなかった。

- **読み方 (鍵を持たない)**: Claude Code は statusline に毎回 `rate_limits` (5 時間・週の使った割合と戻る時刻) を渡す。`scripts/claude-statusline.sh` を statusline にして、それを `limits.file` (`~/.local/state/wakuwaku/rate-limits.json`) に書き留め、Orca の statusline にそのまま渡す。tick の Claude もあなたの Claude も statusline を描くので、どれかが動いていれば記録は新しい
- **決め方** (`internal/limits`、`wk due` と `wk next` が毎回見る):
  - 5 時間: 使った割合が `five_hour_max_pct` (70%) 以上なら止める。あなたの分を 3 割残す
  - 週: 週の経過に合わせた目安 = `seven_day_max_pct` (85%) × (経過の割合 + 0.15)、85% まで。目安以上なら止める。週の初めに使い切らず、週の終わりにも 15% は残す
  - 戻る時刻を過ぎた窓は 0% とみなす
- **記録が古い・ない・壊れている** (`max_age_minutes` 180 分より前): 前と同じ 1 日の予算 (`max_tokens_per_day`) で止める。statusline の設定を Orca が書き換えたときもここに戻る (安全側)
- 1 日の tick の上限 (`ticks_per_day`) は 96 に上げた。量を決めるのは利用上限で、tick の数は暴走止めだけ
- 見る: `wk usage` の 1 行目
