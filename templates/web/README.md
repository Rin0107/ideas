# {{NAME}}

{{DESCRIPTION}}

**{{URL}}**

## これは何
{{GOAL}}

## 使い方

<!-- スクリーンショットと、30 秒でわかる使い方 -->

## データについて

このアプリは重要な情報を持ちません。アカウントはなく、データはお使いの端末の中にだけ保存されます。
詳しくは [DATA.md](DATA.md)。

## 開発

```bash
npm ci
npm run dev      # 手元で動かす
npm run verify   # lint・型・テスト・ビルド・守りの検査
npm run e2e      # ブラウザでの検査 (wrangler dev を起動する)
```

Cloudflare Workers で動いています。

---

[wakuwaku factory](https://github.com/Rin0107/ideas) が自動で作りました (アイディア: `{{IDEA}}`)。
