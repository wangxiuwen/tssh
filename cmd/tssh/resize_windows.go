//go:build windows

package main

import (
	"os"
)

func notifyResize(sigCh chan<- os.Signal) {
	// No SIGWINCH on Windows
}
