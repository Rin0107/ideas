package main

import "time"

// 待ち時間 (テストでは短くする)。
// 既定値は init で入れる: 変数の初期化の式はカバレッジに数えられず、ミューテーションで「テストが通っていない」扱いになるため。
// 既定値そのものは waits_test.go で固定する。
var (
	claudeTimeout  time.Duration // reviewer の claude -p
	mergePoll      time.Duration // CI の結果を見に行く間隔
	mergeWait      time.Duration // CI を待つ長さ
	mergeNoCI      time.Duration // CI が 1 つも出てこないと判断するまで
	checkPoll      time.Duration // Cloudflare のビルドを見に行く間隔
	checkNoRun     time.Duration // check run が 1 つも出ないと判断するまで
	checkWait      time.Duration // Cloudflare のビルドを待つ長さ
	previewE2EWait time.Duration // L のテスト (プレビューの e2e) の結果を待つ長さ (ジョブは 5 分で打ち切られる)
	previewPoll    time.Duration // L のテストの結果を見に行く間隔

	stateWait time.Duration // 状態のロックを待つ長さ (ほかの実行のコマンドが終わるまで)
	statePoll time.Duration // 状態のロックを見に行く間隔

	sysRoot string // /proc と /sys を読む場所 (wk due が機械の余裕を見る。テストでは偽物に向ける)
)

func init() {
	claudeTimeout = 10 * time.Minute
	mergePoll, mergeWait, mergeNoCI = 20*time.Second, 30*time.Minute, 5*time.Minute
	checkPoll, checkNoRun, checkWait = 20*time.Second, 5*time.Minute, 20*time.Minute
	previewE2EWait, previewPoll = 10*time.Minute, 15*time.Second
	stateWait, statePoll = 45*time.Minute, 500*time.Millisecond
	sysRoot = "/"
}
