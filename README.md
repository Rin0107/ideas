# ideas

Goでマネタイズ可能なアプリケーションを作るためのアイディア集。

## 構成

```
ideas/
  idea-template/       # 新規アイディア追加時のテンプレート
    idea-template.md
  0001-log-diff-watcher/
    idea.md
  0002-git-sparse-optimizer/
    idea.md
  ...
```

各アイディアは `ideas/{4桁連番}-{slug}/idea.md` に1つずつ格納する。
フォーマットは `ideas/idea-template/idea-template.md` を参照。

## 現在のフェーズ

- [x] アイディア出し(150個, `0001`〜`0150`)
- [ ] 有望なアイディアの選定
- [ ] Go実装(CLI/サーバーとしてプロトタイプ作成)

Go実装はアイディアごとに着手する際、各ディレクトリに `main.go` を追加していく。
