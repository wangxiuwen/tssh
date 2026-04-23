package shared

import (
	"os/exec"
	"time"
)

// SleepDuration sleeps N seconds. A trivial helper that callers kept writing
// in slightly different ways (time.Duration math) — centralizing makes the
// call sites read the same.
func SleepDuration(seconds int) {
	time.Sleep(time.Duration(seconds) * time.Second)
}

// SleepMs sleeps N milliseconds.
func SleepMs(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// ExecCommand is a thin wrapper around exec.Command. Mainly exists so tests
// can swap the command runner via dependency injection later. Today it's a
// straight passthrough.
func ExecCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
