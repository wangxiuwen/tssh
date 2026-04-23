package main

import "github.com/wangxiuwen/tssh/internal/cmd/k8s"

// cmdKS — delegate to the k8s group (Phase 2). Implementation moved.
func cmdKS(args []string) { k8s.KS(appRuntime, args) }
