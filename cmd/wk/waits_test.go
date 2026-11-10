package main

import (
	"testing"
	"time"
)

// 待ち時間の既定値を固定する (変えるなら、ここと docs を一緒に)。
func TestDefaultWaits(t *testing.T) {
	for name, c := range map[string]struct{ got, want time.Duration }{
		"claudeTimeout": {claudeTimeout, 10 * time.Minute}, "mergePoll": {mergePoll, 20 * time.Second},
		"mergeWait": {mergeWait, 30 * time.Minute}, "mergeNoCI": {mergeNoCI, 5 * time.Minute},
		"checkPoll": {checkPoll, 20 * time.Second}, "checkNoRun": {checkNoRun, 5 * time.Minute},
		"checkWait": {checkWait, 20 * time.Minute}, "previewE2EWait": {previewE2EWait, 10 * time.Minute},
		"previewPoll": {previewPoll, 15 * time.Second},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v (既定は %v)", name, c.got, c.want)
		}
	}
}
