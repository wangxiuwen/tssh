package shared

import (
	"fmt"
	"os"
)

// Fatal prints a user-friendly error + exits 1 if err is non-nil.
// Matches the behaviour callers depend on in cmd/tssh.fatal — same
// emoji, same message format.
func Fatal(err error, msg string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %s: %v\n", msg, err)
		os.Exit(1)
	}
}

// FatalMsg is Fatal's sibling for actionable-error messages that aren't
// wrapped in a Go error. Use for preflight checks (wrong OS, missing
// binary, no root) where the user needs to read + act.
func FatalMsg(msg string) {
	fmt.Fprintln(os.Stderr, "❌ "+msg)
	os.Exit(2)
}
