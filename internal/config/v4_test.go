package config

import (
	"testing"
	"time"
)

func TestV4Defaults(t *testing.T) {
	var v V4
	if v.Min() != 20 || v.Attempts() != 2 || v.CheckTimeout() != 600*time.Second {
		t.Fatal(v.Min(), v.Attempts(), v.CheckTimeout())
	}
	v = V4{MinTasks: 1, TaskAttempts: 5, CheckTimeoutSeconds: 3}
	if v.Min() != 1 || v.Attempts() != 5 || v.CheckTimeout() != 3*time.Second {
		t.Fatal(v.Min(), v.Attempts(), v.CheckTimeout())
	}
}
