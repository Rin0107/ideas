# drop — 捨てて、理由を残す

捨てるのは失敗ではない。**次の選定に効く理由を残す** のが仕事。

## 1. 理由を決める
最新の評価と ticks の記録 (`state/ticks.jsonl`)、blocked の issue を読み、1〜3 行で書く。
- 良い例: 「交差の一意解の検証が Workers の CPU 上限 (10ms) に収まらない。静的に事前計算する形なら成立する」
- 悪い例: 「うまくいかなかった」
二度と選ぶべきでない (核が成立しない・重要な情報が要ると分かった) なら `--never`。

## 2. 片付ける
- 本番を止める (web): アプリの main で `npx wrangler delete --force` は **しない**。鍵が要るので、
  代わりに `reports/weekly/notes.md` に「<名前> の Worker を消す」と書く (retro がまとめて人に頼む)
- ブラウザの外にデータは持っていないはずなので、消すデータはない。D1 を作っていたら同じく notes に書く

## 3. 記録する
```bash
wk drop --reason "<理由>" [--never]
```
repo は archive される (private のまま)。idea.md の履歴に理由が残る。
TICK.md の手順 3。次の tick で新しいアイディアが選ばれる。
