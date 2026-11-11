# 申し送り

tick が気づいたこと・人に頼みたいことを 1 行ずつ。retro がまとめて片付ける。

- 2026-11-11 (ユーザーに頼みたい) 雛形の CI (ci.yml) の security ジョブに `permissions: { contents: read, pull-requests: read }` が要る。private の repo の PR では gitleaks-action が PR のコミット一覧 (pulls/<n>/commits) を読めず 403 で落ち、どの PR も wk merge できない。Rin0107/terminal-sky の PR #6 で 2 回続けて同じ失敗。go-cli と web の雛形の ci.yml と、terminal-sky の ci.yml を直してほしい (守りの部分なのでエージェントは変えられない)
