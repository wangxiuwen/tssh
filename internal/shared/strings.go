package shared

import (
	"encoding/base64"
	"strings"
)

// DecodeOutput tries to base64-decode s; falls back to the original string
// if decoding fails. Cloud Assistant returns output base64-encoded most of
// the time, but the Web UI endpoint in tssh previously bypassed encoding, so
// this function silently handles either.
func DecodeOutput(output string) string {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(output))
	if err != nil {
		return output
	}
	return string(decoded)
}

// TruncateStr returns s with newlines flattened to "\n" and capped at maxLen
// characters. Used to keep single-line table output readable when a cell
// contains a whole command / error message.
func TruncateStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
