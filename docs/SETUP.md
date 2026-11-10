# SETUP — 人が 1 回だけやること

工場を動かす前に、上から順に。所要 20〜30 分。終わったら最後の「動作確認」。
**Cloudflare の鍵はどこにも用意しない。** 配信は Cloudflare の GitHub 連携 (Workers Builds) に任せ、鍵は Cloudflare の中にだけある。
このマシンにある鍵は `gh` のログインだけ。

---

## 1. wk を入れる
```bash
cd ~/src/okuos/repos/ideas
CGO_ENABLED=0 go install ./cmd/wk      # ~/go/bin/wk (PATH に入っている)
wk check                               # ok (アイディア 501 件、アプリ 0 件)
mkdir -p ~/src/wakuwaku ~/.local/state/wakuwaku
```
- フック (guard) も precheck もこの `wk` を使う。**守りの部分 (`cmd/` `internal/` など) を変えたら、必ず入れ直す**
- 入れ直すのは人だけ。エージェントには `go install` させない (guard を書き換えて入れ直す、を防ぐため)

## 2. Cloudflare (Web アプリの配信先。鍵なし)
公開する Web アプリは、配信用の repo `Rin0107/wakuwaku-site` の `public/<名前>/` にまとめて置き、
Cloudflare の GitHub App がその repo を配る (Vercel の GitHub 連携と同じ形)。連携は **この repo 1 つにつき 1 回だけ**。
アプリが増えても、人がやることは増えない。

1. 配信用の repo を作る (private):
   ```bash
   cd ~/src/okuos/repos/ideas && wk site init
   ```
2. Cloudflare のアカウントを用意する。**工場専用のアカウントを新しく作るのがおすすめ** (https://dash.cloudflare.com/sign-up 、
   クレジットカードは登録しない)。Cloudflare が自動で作るビルド用の鍵は Workers・KV・R2 を書き換えられるので、
   ほかの用途と同じアカウントだと、壊れたときの範囲が広くなる
3. **Workers & Pages → Create → Import a repository → GitHub と連携**
   - GitHub App のインストール画面で **Only select repositories → `Rin0107/wakuwaku-site` だけ** を選ぶ
     (工場の repo やアプリの repo は Cloudflare に見せない)
   - 設定:
     | 項目 | 値 |
     |---|---|
     | Project name | `wakuwaku-site` (wrangler.jsonc の name と同じにする) |
     | Production branch | `main` |
     | Build command | **空のまま** (public/ は wk が手元でビルドしたもの。Cloudflare でアプリのコードを動かさない) |
     | Deploy command | `npx wrangler deploy` (そのまま) |
     | Non-production branch builds | **有効** (`preview` ブランチがプレビューになる。評価はここでする) |
     | Root directory | `/` |
     | API token | 自動で作られるもののまま (気になるなら、Workers Scripts の編集だけのカスタムトークンを選ぶ) |
4. 最初のデプロイが終わったら、出た URL (`https://wakuwaku-site.<サブドメイン>.workers.dev`) を開き、
   一覧のページが出ることを確かめて、`factory.json` の `site.url` に書いてコミットする:
   ```bash
   cd ~/src/okuos/repos/ideas
   $EDITOR factory.json        # "site": { "repo": "Rin0107/wakuwaku-site", "url": "https://wakuwaku-site.<サブドメイン>.workers.dev" }
   git commit -am "site.url を設定" && git push
   ```
   `site.url` が空のあいだ、工場は Web アプリをプレビューにも本番にも出さない (CLI のアプリは作れる)

しくみ (docs/DESIGN.md §6・§7.4):
- `wk deploy`: アプリを手元で鍵なしにビルドし、`preview` ブランチの `public/<名前>/` に置く → Cloudflare がプレビューに出す →
  wk がプレビューの URL で守りのヘッダーと中身を確かめる
- `wk ship`: 同じものを `main` に置く → 本番 `https://wakuwaku-site.<サブドメイン>.workers.dev/<名前>/`
- `wk drop`: `main` から `public/<名前>/` を消す (本番からも消える)
- 配信用の repo を書くのは wk だけ (手元の写しは `~/.local/state/wakuwaku/site`。エージェントは guard で入れない)

## 3. GitHub
- `gh auth status` で Rin0107 にログイン済みであること (scope: `repo`)。工場はこのトークンで private の repo を作り、issue・PR を扱う
- scope に `workflow` も要る (アプリの repo に CI の定義を push するため): `gh auth refresh -h github.com -s workflow`
- 工場の repo: `Rin0107/ideas` (private)。remote は https で、認証は gh に任せる:
  `git -C ~/src/okuos/repos/ideas config --local credential.helper '!gh auth git-credential'`
- 工場の repo に incident 用のラベルを作る: `gh label create wk:incident --repo Rin0107/ideas --color d93f0b`
- (推奨・あとで) 工場専用の fine-grained token にすると、壊れたときの範囲がさらに狭くなる。今は `gh` のトークンを共有している

## 4. Claude Code の信頼済み設定
Orca の定期実行は毎回新しい worktree で claude を起動する。元の checkout を信頼済みにしておかないと「信頼しますか」で止まる
(okuos の `notes/orca-automation-trust.md`):
```bash
jq '.projects["/home/user/src/okuos/repos/ideas"].hasTrustDialogAccepted = true' ~/.claude.json > ~/.claude.json.tmp && mv ~/.claude.json.tmp ~/.claude.json
```

## 5. (任意) 手元で Playwright を動かす
NixOS では `npx playwright install` のブラウザが動かない。e2e は CI (Ubuntu) で必ず動くので、手元は無くても回る。
手元でも動かしたいときは home-manager に足す (`@playwright/test` と版を合わせる。今は 1.65.0):
```nix
home.packages = [ pkgs.playwright-driver.browsers ];
home.sessionVariables = {
  PLAYWRIGHT_BROWSERS_PATH = "${pkgs.playwright-driver.browsers}";
  PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS = "true";
};
```
evaluate の段階はプレビューを Playwright で触るので、入れておくと評価の質が上がる。

## 6. 定期実行を登録する
```bash
orca-ide repo add --path '\\wsl.localhost\NixOS\home\user\src\okuos\repos\ideas'
# 出てきた repo の id を使う
orca-ide automations create --name "wakuwaku tick" --provider claude --timezone Asia/Tokyo \
  --trigger "0 8,11,14,17,20,23 * * *" --repo id:<repo の id> \
  --precheck "wsl.exe -d NixOS -- /home/user/go/bin/wk due" \
  --prompt "harness/TICK.md を読んで、その通りにやって"
```
precheck は Windows の cmd.exe で動くので、`wsl.exe -d NixOS --` を付ける (付けないと毎回 exit 1 で飛ばされる)。
最初の 1 回は、登録後に手で動かして見届ける: `orca-ide automations run --id <automation の id>`

---

## 動作確認
```bash
wk status          # 次の段階: select (作っているアプリがない)
wk due             # ok (今日 0/6 tick)
```
最初の数 tick は `state/ticks.jsonl` と、作られた repo の issue・PR を見る。

## 止める・再開する
```bash
touch ~/.local/state/wakuwaku/STOP      # 止める (次の tick から動かない)
rm ~/.local/state/wakuwaku/STOP         # 再開する
```
連続で失敗すると、工場が自分で STOP を置き、`Rin0107/ideas` に `wk:incident` の issue を立てる。
直したら STOP を消す。朝のブリーフ (okuos) にも「停止中」と出る。

## おかしなものが公開されたかもしれないとき
1. `touch ~/.local/state/wakuwaku/STOP`
2. Cloudflare の Workers & Pages → wakuwaku-site → Deployments で、前の版に **Rollback**
   (全部止めるなら Settings → Build で連携を外す)
3. `gh auth refresh` で GitHub のトークンを作り直す (このマシンにある鍵はこれだけ)
4. 原因を直してから STOP を消す
