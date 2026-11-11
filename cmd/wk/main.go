// wk は wakuwaku factory の制御 CLI。
// 「次に何をするか」「合格か」「止まるか」を決めるのはこの CLI で、LLM は各段階の中身だけをやる。
// 設計: docs/DESIGN.md
package main

import (
	"fmt"
	"io"
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
	"propose": {"--plan <json>: 見回り (patrol) の中で、ロールの提案を issue にする (wk:proposal)", cmdPropose},
	"triage":  {"--decisions <json>: leader が提案を仕分ける (now / later / reject)", cmdTriage},
	"labels":  {"<repo>: 工場のラベル (wk:*・role:*) をそろえる (portfolio の repo だけ。ラベルが増えたあと古いアプリに)", cmdLabels},

	"review": {"<repo> <pr>: ロールごとに別の Claude が PR を見る (SRE・engineer・UI なら designer)。全員 approve なら wk:reviewed", cmdReview},
	"merge":  {"<repo> <pr>: CI 全部 green かつ wk:reviewed のときだけ squash merge", cmdMerge},
	"deploy": {"作っているアプリを出す: Web はプレビュー (配信用 repo の preview)、CLI はタグとリリース", cmdDeploy},
	"site":   {"init: 配信用の repo を作る (人が 1 回だけ。docs/SETUP.md)", cmdSite},
	// フック
	"guard-bash": {"PreToolUse(Bash) フック", cmdGuardBash},
	"guard-edit": {"PreToolUse(Edit|Write) フック", cmdGuardEdit},
}

func main() { os.Exit(run(os.Args[1:], os.Stderr)) }

// run は main の中身。終了コードを返す (0: 成功、1: 失敗、2: 使い方の誤り、exitError はその code)。
func run(args []string, stderr io.Writer) int {
	if len(args) == 0 || commands[args[0]].run == nil {
		usage(stderr)
		return 2
	}
	if err := commands[args[0]].run(args[1:]); err != nil {
		fmt.Fprintln(stderr, "wk "+args[0]+":", err)
		if e, ok := err.(exitError); ok {
			return e.code
		}
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "使い方: wk <コマンド>")
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %-11s %s\n", n, commands[n].usage)
	}
}

type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }
