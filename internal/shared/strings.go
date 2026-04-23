package shared

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
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

// DefaultStr returns dflt when s is empty, otherwise s. Lets callers avoid
// the empty-string-looks-like-data problem (e.g. blank CPU from a missing
// metrics-server should read "-" not nothing).
func DefaultStr(s, dflt string) string {
	if s == "" {
		return dflt
	}
	return s
}

// FormatDuration — short human duration (30s / 5m / 2.5h / 3.2天). Used all
// over arms / health / top where a single-cell render needs to fit tightly.
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1f天", d.Hours()/24)
}
