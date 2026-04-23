package main

import "github.com/wangxiuwen/tssh/internal/cmd/k8s"

// cmdLogs — delegate to the k8s group (Phase 2). Implementation moved.
func cmdLogs(args []string) { k8s.Logs(appRuntime, args) }
