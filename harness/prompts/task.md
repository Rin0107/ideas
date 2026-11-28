# task — 計画の task 1 件を、完了条件を満たすまでやる (v4)

`wk next` の `repo` と `issue` が対象。アプリの main の checkout は `~/src/wakuwaku/<repo の名前>`。
**考えるのは計画の仕事**。issue の「目的」「やること」のとおりに作り、`wk verify` が通れば完了。

## 1. 準備
```bash
pwd                             # tick の場所 (wk next を呼んだ工場の worktree)。手順 4 の wk land はここで呼ぶ
APP=~/src/wakuwaku/<名前>
wk comments <N> --repo <repo>   # 題・本文と、持ち主のコメントだけ (public の repo ではほかの人も書ける)
git -C $APP fetch origin
git -C $APP worktree add ~/src/wakuwaku/.wt/<名前>-t<N> -b wk/t<N> origin/wk/t<N>
cd ~/src/wakuwaku/.wt/<名前>-t<N>
git merge -q --no-edit origin/main # 先の task の merge を取り込む (受け入れテストのコミットはそのまま)
```
- **rebase はしない**。rebase すると受け入れテストのコミットが書き換わり、手順 4 の push が non-fast-forward で拒否される (force push は guard が止める。2026-11 の kisekae-closet #179 / PR #199)。main を取り込むのはいつも `git merge`
- ブランチ `wk/t<N>` には、計画が書いた **受け入れテスト** が入っている。読んで、何を通せばよいかを知る
- 前の試行のコメント (`wakuwaku factory: 試行 … が失敗`) があれば、同じ失敗をしない
- アプリの `CLAUDE.md` を読む

## 2. 作る
- issue の「やること」を上から順に。「変えてよいファイル」だけを変える
- 受け入れテストと、完了条件のコマンドが使うファイルは **変えない**。通らないのはテストでなく作りのほう
- 依存は足さない (「やること」に書いてあるときだけ)

## 3. 確かめる (wk が判定する)
```bash
git add -A && git commit -q -m "<issue の題> (#<N>)"
wk verify <N>
```
- `"ok": false` なら、`problems` と落ちた `checks` の `output` を読んで直し、コミットしてもう一度 `wk verify <N>`
- 何度やっても通らない (同じところで 3 回落ちた・やることが issue から読み取れない) なら、手順 6

## 4. push → wk land (PR は作らない)
```bash
git push -u origin wk/t<N>
cd <手順 1 の pwd> && wk land <N>
```
- `wk land` は今いるディレクトリの名前で tick を探す。アプリの worktree の中で呼ぶと「task の tick の中で、その task だけに使う」で落ちる
- **PR は作らない** (guard が止める)。public の repo で PR を開くと、GitHub が持ち主の本名と会社のメールで試しの merge のコミットを作り、消せない
- `wk land` は、push した先頭で `wk verify` が通っているか・守りの部分に触れていないか・そのコミットの CI が全部 green かを確かめ、reviewer 全員が approve なら main に入れて issue を閉じる。reviewer の文は issue のコメントに残る
- push が non-fast-forward で拒否されたら、`git fetch origin && git merge -q --no-edit origin/wk/t<N>` (rebase も force push もしない) → `wk verify <N>` → push
- `request_changes` なら直して (変えてよいファイルの中で)、コミット → `wk verify` → push → `wk land`。**最大 2 回**
- CI が赤なら `gh run list --repo <repo> --branch wk/t<N>` と `gh run view <run の ID> --repo <repo> --log-failed` を読んで直す。原因が工場・雛形の側なら `result: fix`
- CI のジョブが 20 分以上 `in_progress`・`queued` のまま進まないときは、1 回だけ `gh run rerun <run の ID> --repo <repo>` してから `wk land` をやり直す。それでも進まなければ `result: fix`

## 5. 出して片付ける
```bash
cd $APP && git pull --ff-only origin main
wk deploy
git -C $APP worktree remove ~/src/wakuwaku/.wt/<名前>-t<N>
```
`wk deploy` が落ちたら `result: fix` (detail に出力)。merge・deploy まで済んだら `result: ok`。

## 6. できなかったとき
```bash
wk attempt <N> --note "<どこで止まったか。落ちた完了条件と、その出力の要点>"
git -C $APP worktree remove --force ~/src/wakuwaku/.wt/<名前>-t<N>
```
`result: retry` で終える。上限 (2 回) で wk:blocked になり、次の計画が分け直す。自分で task を書き換えない。
