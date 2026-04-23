package main

import (
	"os/exec"

	"github.com/wangxiuwen/tssh/internal/shared"
)

// Thin wrappers over internal/shared. Kept around so the many cmd/tssh
// files that call these by lowercase name keep compiling during the split.
// New files should reach into internal/shared directly.
func sleepDuration(seconds int) { shared.SleepDuration(seconds) }
func sleepMs(ms int)            { shared.SleepMs(ms) }
func findFreePort() int         { return shared.FindFreePort() }

func execCommand(name string, args ...string) *exec.Cmd {
	return shared.ExecCommand(name, args...)
}
