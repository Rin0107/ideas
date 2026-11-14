# terminal-sky の評価に使ったリリースが main より古い

## 何が起きたか
2026-11-14 の evaluate (Rin0107/terminal-sky、MVP) で、`wk status` の URL
https://github.com/Rin0107/terminal-sky/releases/tag/v0.1.0 を採点した。
sre の報告: 「リリースのタグは 34f24fff を指していて main より 2 コミット古く、sky tonight・惑星と月・都市が入っていない」
閉じた issue #3・#4・#5 (PR #12・#13 で main に入った) がリリースにない。main では go run で tonight・cities・惑星が動く。
その結果 core_works=false になり、wk evaluate の結論は drop (「体験の核が本番で動かない」)。

## 原因 (見立て)
go-cli の evaluate の前に、最新の main からリリースを作り直していない。
v0.1.0 は 2026-11-12 の wk deploy で作られ、そのあと MVP の issue が main に入った。
evaluate の材料 (wk status の URL) が古いリリースのまま。

## どう直すか
- evaluate (go-cli) の手順 1 で、リリースのタグが main の HEAD を指していなければ、先に wk deploy で作り直す
  (または wk next が evaluate を出す前に、main より古いリリースなら deploy の段を挟む)
- 今回の terminal-sky は、古いリリースで付いた drop の前に、main から作り直したリリースで評価し直すのが妥当
