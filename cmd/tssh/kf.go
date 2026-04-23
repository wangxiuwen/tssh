package main

import "github.com/wangxiuwen/tssh/internal/cmd/k8s"

// cmdKF — delegate to the k8s group (Phase 2). Implementation moved.
func cmdKF(args []string) { k8s.KF(appRuntime, args) }
