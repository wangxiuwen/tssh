//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func notifyResize(sigCh chan<- os.Signal) {
	signal.Notify(sigCh, syscall.SIGWINCH)
}
