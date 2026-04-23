//go:build windows

package session

import (
	"os"
)

func notifyResize(sigCh chan<- os.Signal) {
	// No SIGWINCH on Windows
}
