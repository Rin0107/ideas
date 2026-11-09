# SETUP — 人が 1 回だけやること

工場を動かす前に、上から順に。所要 20〜30 分。終わったら最後の「動作確認」。
**鍵 (トークン) はどの repo にもコミットしない。この手順のとおり `~/.config/wakuwaku/env` にだけ置く。**

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

## 2. Cloudflare (Web アプリの配信先)
1. https://dash.cloudflare.com/sign-up で無料のアカウントを作る (クレジットカードは登録しない)
2. **Workers & Pages** を一度開き、`*.workers.dev` のサブドメインを決める (アプリの URL が `https://<名前>.<サブドメイン>.workers.dev` になる)
3. **My Profile → API Tokens → Create Token → Custom token**
   | 項目 | 値 |
   |---|---|
   | Permissions | `Account` / `Workers Scripts` / `Edit` だけ |
   | Account Resources | Include / このアカウントだけ |
   | Zone Resources | なし (ドメインは使わない) |
   | TTL | 90 日 (切れたら作り直す。retro が期限を知らせる) |
   - D1・KV・R2 の権限は付けない。アプリが匿名の集計を持つ必要が出たら、そのとき人が足す
   - 「Edit Cloudflare Workers」のテンプレートは権限が広い (Routes・KV・R2 など) ので使わない
4. アカウント ID (ダッシュボードの右側) とトークンを、env ファイルに書く:
```bash
install -d -m 700 ~/.config/wakuwaku
install -m 600 /dev/null ~/.config/wakuwaku/env
$EDITOR ~/.config/wakuwaku/env
```
```
CLOUDFLARE_ACCOUNT_ID=…
CLOUDFLARE_API_TOKEN=…
```
- `wk deploy` だけがこのファイルを読み、wrangler に環境変数で渡す。権限が 600 でないと wk が止まる
- エージェントはこのファイルを読めない (guard が `.config/wakuwaku` を含むコマンドを止める)
- nix-config・okuos・ideas のどこにも書かない

## 3. GitHub
- `gh auth status` で Rin0107 にログイン済みであること (scope: `repo`)。工場はこのトークンで private の repo を作り、issue・PR を扱う
- 工場の repo: `Rin0107/ideas` (private)。ない場合は作る: `gh repo create Rin0107/ideas --private --source ~/src/okuos/repos/ideas --push`
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
evaluate の段階は本番を Playwright で触るので、入れておくと評価の質が上がる。

## 6. 定期実行を登録する
```bash
orca-ide repo add --path '\\wsl.localhost\NixOS\home\user\src\okuos\repos\ideas'
# 出てきた repo の id を使う
orca-ide automations create --name "wakuwaku tick" --provider claude --timezone Asia/Tokyo \
  --trigger "0 8,11,14,17,20,23 * * *" --repo id:<repo の id> \
  --precheck "/home/user/go/bin/wk due" \
  --prompt "harness/TICK.md を読んで、その通りにやって"
```
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

## 鍵が漏れたかもしれないとき
1. Cloudflare の API Tokens で、そのトークンを **Roll** か **Delete**
2. `touch ~/.local/state/wakuwaku/STOP`
3. `gh auth refresh` で GitHub のトークンも作り直す
4. 新しい鍵を `~/.config/wakuwaku/env` に書き、STOP を消す
