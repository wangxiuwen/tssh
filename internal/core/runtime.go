// Package core defines the runtime contract that per-group command packages
// (internal/cmd/k8s, internal/cmd/net, ...) depend on — config loading,
// instance resolution, cache access. The legacy cmd/tssh main wires up a
// concrete Runtime at startup; smaller binaries (tssh-k8s, tssh-net, ...)
// wire the same Runtime with a different subset of subcommands registered.
//
// Why not import cmd/tssh directly: cmd/tssh is package main and can't be
// imported by other mains. Before this contract existed, every group would
// need to duplicate the resolve/load/cache logic. Now they each take a
// Runtime and stay decoupled.
package core

import "github.com/wangxiuwen/tssh/internal/model"

// Runtime is what a command group needs to do its job.
// It is INTENTIONALLY small; anything bigger belongs in its own group.
type Runtime interface {
	// LoadConfig returns Aliyun credentials + region for the active profile.
	// Fatal-exits on error so callers don't need to boilerplate-wrap it.
	LoadConfig() *model.Config

	// ResolveInstance maps a name / ID / index / pattern to an Instance.
	// Returns nil on ambiguity / not-found after printing a user message;
	// fatal-exits on cache I/O problems.
	ResolveInstance(name string) *model.Instance

	// LoadAllInstances returns the current instance cache (syncing if stale).
	// Empty slice is valid (no instances); error is exceptional.
	LoadAllInstances() []model.Instance

	// ExecOneShot runs cmd via Cloud Assistant and blocks until it returns.
	// Output is already base64-decoded. timeoutSec applies server-side + ~10s
	// slack for local polling.
	ExecOneShot(instanceID, cmd string, timeoutSec int) (*ExecResult, error)

	// ExecInteractive opens a WebSocket session and streams stdin/stdout
	// against the remote shell / command. Used for `kubectl logs -f` and
	// similar long-lived flows. Blocks until the session closes.
	ExecInteractive(instanceID, cmd string) error
}

// ExecResult mirrors the one-shot output from Cloud Assistant. Kept here
// instead of importing model.CommandResult so subcommand groups don't have to
// know about the SDK types.
type ExecResult struct {
	Output   string
	ExitCode int
}

// Registered subcommand. Each group returns one or more of these from a
// RegisterFor(Runtime) constructor; the main binary appends them into a
// single dispatch table.
type Command struct {
	Name    string            // "ks" / "fwd" / ...
	Aliases []string
	Group   string            // "k8s" / "net" / "db" / "arms" / "core"
	Desc    string            // one-line
	Run     func(args []string)
}
