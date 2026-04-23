//go:build !windows

package session

import (
	"os"
	"os/signal"
	"syscall"
)

func notifyResize(sigCh chan<- os.Signal) {
	signal.Notify(sigCh, syscall.SIGWINCH)
}
