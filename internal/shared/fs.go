package shared

import "os"

// FileExists reports whether path exists (file, directory, symlink target,
// whatever). Errors other than "not found" count as "exists" so callers
// don't silently skip files they can't stat for permissions reasons.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsTerminal returns true when stdin is attached to a terminal rather than
// being piped/redirected. Used to decide when to show interactive prompts
// vs fall back to non-interactive defaults.
func IsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return true
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
