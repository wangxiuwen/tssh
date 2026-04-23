// Command tssh-k8s is the k8s-only slice of the tssh toolkit. It ships the
// same ks / kf / logs / events subcommands as the full tssh binary but
// without anything else — smaller download, same source.
//
// Subcommands:
//
//	tssh-k8s ks    <jump> <svc>  [-n ns] [-j]             service diagnostics
//	tssh-k8s kf    <jump> <svc:port> ...                  port-forward multi-svc
//	tssh-k8s logs  <jump> <svc> [-n ns] [-l sel] [-f]     multi-pod log stream
//	tssh-k8s events <jump> [-n ns] [-w]                   k8s events viewer
//
// The main tssh binary continues to expose everything including these; this
// binary exists for users who only want the k8s slice (or want a separate
// permissions/version boundary).
package main

import (
	"fmt"
	"os"

	"github.com/wangxiuwen/tssh/internal/cmd/k8s"
	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/model"
)

// TODO(refactor): wire up a real k8s Runtime once core helpers (config/cache
// loading) are factored out of cmd/tssh. For now this is a compile-only
// stub — calling `tssh-k8s <sub>` prints a TODO and exits 1.  The purpose
// of the stub is to prove the package structure supports independent mains.
type stubRuntime struct{}

func (stubRuntime) LoadConfig() *model.Config                                      { return nil }
func (stubRuntime) ResolveInstance(string) *model.Instance                          { return nil }
func (stubRuntime) LoadAllInstances() []model.Instance                              { return nil }
func (stubRuntime) ExecOneShot(string, string, int) (*core.ExecResult, error)       { return nil, errNotWired }
func (stubRuntime) ExecInteractive(string, string) error                            { return errNotWired }
func (stubRuntime) StartPortForward(string, int, int) (func(), error)               { return nil, errNotWired }
func (stubRuntime) StartSocatRelay(string, string, int) (int, func(), error)        { return 0, nil, errNotWired }

var errNotWired = fmtErr("tssh-k8s runtime 尚未接通; Phase 3 会把 cmd/tssh 的 tsshRuntime 挪到 internal/runtime 供两个 main 共用")

type errString string

func (e errString) Error() string { return string(e) }
func fmtErr(s string) error       { return errString(s) }

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}
	var rt core.Runtime = stubRuntime{}
	switch args[0] {
	case "ks":
		k8s.KS(rt, args[1:])
	case "kf":
		k8s.KF(rt, args[1:])
	case "logs":
		k8s.Logs(rt, args[1:])
	case "events":
		k8s.Events(rt, args[1:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", args[0])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`tssh-k8s — k8s-only slice of tssh

Subcommands:
  tssh-k8s ks     <jump> <svc>  [-n ns] [-j]
  tssh-k8s kf     <jump> <svc:port>[=<local>] ...  [--browser] [-j]
  tssh-k8s logs   <jump> <svc>  [-n ns] [-l sel] [-f]
  tssh-k8s events <jump>        [-n ns] [--svc x] [--level Warning] [-w]

Runtime wiring is in progress; until Phase 3 lands, use the main tssh binary
for actual remote calls. This binary demonstrates the cmd/k8s package can
be linked standalone.`)
}
