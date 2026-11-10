# wakuwaku-site

わくわく工場 ([Rin0107/ideas](https://github.com/Rin0107/ideas)) が公開した Web アプリを、まとめて配る場所。

- `public/<名前>/` が 1 つのアプリ。`https://<この Worker>/<名前>/` で開く
- 書くのは `wk` だけ (`wk deploy` が preview ブランチに、`wk ship` が main に)。手で編集しない
- 配信は Cloudflare の GitHub 連携 (Workers Builds)。main → 本番、preview → プレビュー
- 鍵はどこにも置かない。ビルドのコマンドはなく、Cloudflare で動くのは版固定の wrangler だけ
- サーバーの処理はない。どのアプリも、データは利用者のブラウザの中にだけ置く
