package main

import (
	"os/exec"
	"time"

	"github.com/wangxiuwen/tssh/internal/shared"
)

func sleepDuration(seconds int) {
	time.Sleep(time.Duration(seconds) * time.Second)
}

func sleepMs(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// findFreePort keeps the old name as a delegate to shared.FindFreePort so
// callers in cmd/tssh migrate at their own pace.
func findFreePort() int { return shared.FindFreePort() }

func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
