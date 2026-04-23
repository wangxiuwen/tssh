// Package shared holds small utilities used across command subsystems so that
// individual tssh binaries (tssh-core / tssh-net / tssh-k8s / tssh-db / ...)
// can each pull in only what they need from a library package, while the
// legacy tssh super-binary keeps working by re-exporting everything via
// package main.
//
// Keep this package leaf-level: no imports of cmd/tssh, no aliyun/model
// touching — only pure helpers. Anything that needs config or the SDK belongs
// in its own internal/<group> package.
package shared

import "strings"

// ShellQuote escapes a string for safe inclusion inside single quotes in a
// POSIX shell command. Callers still wrap it in single quotes themselves,
// e.g. fmt.Sprintf("cmd '%s'", ShellQuote(x)).
//
// Mirrors the behaviour of the original cmd/tssh.shellQuote.
func ShellQuote(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}
