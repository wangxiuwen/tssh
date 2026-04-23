package main

import "github.com/wangxiuwen/tssh/internal/cmd/k8s"

// cmdEvents delegates to the internal/cmd/k8s package. This file used to
// hold the implementation; it was moved in Phase 2 of the multi-binary
// refactor so that a future `tssh-k8s` binary can link the group directly
// without pulling in cmd/tssh.
func cmdEvents(args []string) { k8s.Events(appRuntime, args) }
