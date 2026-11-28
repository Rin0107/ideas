# plan — 今夜の task を作る (v4)

評価のあと (または最初の版) に、次の版の **task を 20 件以上** 作る。夜の tick (Sonnet) は 30 分に 1 回、14 回動く。
1 件は **1 tick (30 分) で作り切れる** 大きさ。Sonnet は考えずに、書いてあるとおりにやる。

## 1. 読む
- 最新の評価 (`reports/evaluations/<repo>-<n>.md`): 「一番の弱点」と「バグ」
- idea.md の全文と `## 履歴`
- 昨夜の task の結果: `gh issue list --repo <repo> --milestone <今のマイルストーン> --state all --json number,title,state,labels`。
  `wk:blocked` のものはコメント (`試行 … が失敗`) を `wk comments <N> --repo <repo>` で読み、**なぜ Sonnet が詰まったか** を知る (大きすぎた・やることが曖昧・テストが厳しすぎた)
  - 原因が **受け入れテストの側** (待ちが足りない・環境で揺れる・前のテストと矛盾) で、作りは issue どおりなら、作り直さずにテストだけ直す:
    テストのブランチ (`origin/wk/t<N>`) の worktree でテストを直し、`wk retest <N> --dir <worktree> --reason "<なぜ>"`。
    wk が「直したテストも今の main では落ちる」を確かめ、ブランチに足して sha を書き直し、blocked と試行の数を外す。tick の中では使えない (11-24 の #107 は AC-3 が setTimeout(0) 1 回の待ちで、CI だけ落ちた)
- **人の issue**: `wk inbox`。持ち主が朝に書いた「思ったこと・やってほしいこと」。**全部** を task にするか、しない理由を書く (wk plan が漏れを止める)
  - task にしたものは `from` に `owner/repo#n` を書く。wk plan がその issue に task の番号を書いて閉じる
  - しないもの (安全・お金の決まりに反する・意味が取れない) は `declined` に理由を書く。wk plan が理由を書いて閉じる

## 2. 版の goal を決める
「一番の弱点」と人の issue から、今夜で成立させる体験を 1〜2 文に。`[security]` のバグがあれば最初の task にする。

## 3. 受け入れテストを先に書く
アプリの worktree を作り、**task ごとに、今は通らない受け入れテスト** を書く。完了条件はこのテストとコマンドで決まる。
```bash
APP=~/src/wakuwaku/<名前>
git -C $APP fetch origin
git -C $APP worktree add ~/src/wakuwaku/.wt/<名前>-plan --detach origin/main
cd ~/src/wakuwaku/.wt/<名前>-plan && npm ci
```
- テストはアプリの決まり (vitest・Playwright など) で書く。**task ごとに別のファイル** (例 `tests/plan/v0.10-03-weather-fast.test.ts`)
- 完了条件は **定量的で、コマンドの終了コードで判定できる** もの。例:
  - `npx vitest run tests/plan/v0.10-03-weather-fast.test.ts` (天気の取得が 2 秒以内、を測るテスト)
  - `npm run verify` (今までのテストと lint・型が通る。どの task にも入れる)
  - `test $(wc -c < dist/assets/index.js) -lt 300000`
  - 「見た目がよい」のような、測れない条件は書かない。測れる形 (要素がある・色のコントラスト比 ≥ 4.5・360px で横にはみ出さない) に直す
- 書いたら、そのテストが **今は落ちる** ことを自分で走らせて確かめる (wk plan も確かめ、通るものがあれば登録しない)
- テストは `.github/` `wk.json` `.claude/` などの守りの部分に置かない
- **前の受け入れテストと矛盾しないか** 確かめる (`tests/plan/`・`e2e/` を、変える画面や関数の名前で grep)。両立しないなら、どちらを優先するか決め、古いテストのファイルを `paths` に入れて、steps に直し方 (どの AC をどう変えるか) を書く。11-21 の #22 は、古い e2e (AC-44-1) が変えてよいファイルの外にあり、2 回とも通せなかった

## 4. task に分ける
1 件ごとに:
- `title`: 何をするか (短く)
- `purpose`: 目的 1〜2 文 (どの弱点・どの人の issue のためか)
- `steps`: やること。**ファイル名・関数名・値まで具体的に**。Sonnet が設計を考えなくてよいように。1 件 3〜8 手
- `checks`: 完了条件。`name` と `run` (アプリの root で実行するコマンド)。そのテストのコマンドと `npm run verify` を必ず入れる
- `tests`: その task の受け入れテストのファイル (worktree の root からの相対)
- `paths`: Sonnet が変えてよいファイル (`src/**`、`index.html` など)。広げすぎない
- `after`: 先に終わっていてほしい task の **添字** (0 から。前の task だけ)
- `from`: 元の人の issue (あれば)
- `role`: 画面なら `designer`、安全・壊れ方なら `sre` (省けば engineer)

大きすぎる (1 tick で終わらない・steps が 8 手を超える) ものは分ける。昨夜 blocked になったものは、理由を見て小さく分け直す。
20 件に届かなければ、弱点に効く磨き (速さ・分かりやすさ・スマホ幅・失敗の表示) で埋める。水増しの task (中身のないテストだけ) は作らない。

## 5. 登録する
```json
{"goal": "朝、天気を待たずに 3 秒で今日の服が決まる",
 "setup": "npm ci",
 "tasks": [{"title": "…", "purpose": "…", "steps": ["…"], "checks": [{"name": "天気が 2 秒以内", "run": "npx vitest run tests/plan/v0.10-01-weather.test.ts"}, {"name": "全体", "run": "npm run verify"}],
            "tests": ["tests/plan/v0.10-01-weather.test.ts"], "paths": ["src/**"], "after": [], "from": ["Rin0107/ideas#40"]}],
 "declined": [{"ref": "Rin0107/kisekae-closet#50", "reason": "位置情報を保存することになる (DESIGN §7)"}]}
```
```bash
wk plan --plan /tmp/wk-plan-<名前>-<日時>.json --dir ~/src/wakuwaku/.wt/<名前>-plan
```
- wk は形を検査し、テストが今は落ちることを確かめてから、issue (マイルストーン v0.<n>) とテストのブランチ (`wk/t<issue>`) を作る。
  前の計画の残りの issue と、人の issue は wk が閉じる
- 落ちたら、出たエラーのとおりに直して、もう一度 `wk plan`
- 終わったら worktree を片付ける: `git -C $APP worktree remove --force ~/src/wakuwaku/.wt/<名前>-plan`

PLAN.md の手順 3 で記録する (note は「計画 v0.<n>: <goal> (task <件数>)」)。
