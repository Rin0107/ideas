// wk は wakuwaku factory の制御 CLI。
// 「次に何をするか」「合格か」「止まるか」を決めるのはこの CLI で、LLM は各段階の中身だけをやる。
// 設計: docs/DESIGN.md
package main

import (
	"fmt"
	"os"
	"sort"
)

type command struct {
	usage string
	run   func(args []string) error
}

var commands = map[string]command{
	// tick の枠
	"due":  {"tick を始めてよいか (precheck)。止める理由があれば終了コード 1", cmdDue},
	"next": {"ロックを取り、次の段階を JSON で出す", cmdNext},
	"done": {"--result ok|fail [--note ...]: tick を終える (記録・ロック解除)", cmdDone},
	"sync": {"今の worktree のブランチを工場の main に merge して push する", cmdSync},
	// 状態
	"status":     {"[--short] 工場の状態", cmdStatus},
	"check":      {"状態・アイディア・設定の矛盾を検査 (CI でも使う)", cmdCheck},
	"candidates": {"[-n 15] 選定の候補を JSON で出す", cmdCandidates},
	// 段階ごとの確定 (LLM の出力を受け取り、ルールで決める)
	"select":   {"--scores <json>: 点数から 1 件選び portfolio に足す (status=selected)", cmdSelect},
	"scaffold": {"--plan <json>: 雛形から repo を作り、マイルストーンと issue を登録 (status=building)", cmdScaffold},
	"attempt":  {"<issue> --note ...: build の失敗を issue に記録 (上限で wk:blocked)", cmdAttempt},
	"evaluate": {"--scores <json>: 評価の点数から ship / iterate / drop を決める", cmdEvaluate},
	"iterate":  {"--plan <json>: 次のマイルストーンと磨く issue を登録 (status=iterating)", cmdIterate},
	"ship":     {"[--url <url>]: 公開の点検をして本番に出し public にする (Web は URL を wk が決める。CLI は release の URL)", cmdShip},
	"drop":     {"--reason ... [--never]: 本番から消し、archive して捨てる (status=dropped)", cmdDrop},
	// PR
	"review": {"<repo> <pr>: 別の Claude に PR を見せ、approve なら wk:reviewed を付ける", cmdReview},
	"merge":  {"<repo> <pr>: CI 全部 green かつ wk:reviewed のときだけ squash merge", cmdMerge},
	"deploy": {"作っているアプリを出す: Web はプレビュー (配信用 repo の preview)、CLI はタグとリリース", cmdDeploy},
	"site":   {"init: 配信用の repo を作る (人が 1 回だけ。docs/SETUP.md)", cmdSite},
	// フック
	"guard-bash": {"PreToolUse(Bash) フック", cmdGuardBash},
	"guard-edit": {"PreToolUse(Edit|Write) フック", cmdGuardEdit},
}

func main() {
	if len(os.Args) < 2 || commands[os.Args[1]].run == nil {
		usage()
		os.Exit(2)
	}
	if err := commands[os.Args[1]].run(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "wk "+os.Args[1]+":", err)
		if e, ok := err.(exitError); ok {
			os.Exit(e.code)
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "使い方: wk <コマンド>")
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(os.Stderr, "  %-11s %s\n", n, commands[n].usage)
	}
}

type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }
