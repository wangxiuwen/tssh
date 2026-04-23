package main

import cmdnet "github.com/wangxiuwen/tssh/internal/cmd/net"

// cmdRun — delegate to internal/cmd/net (Phase 2 net-group extraction).
func cmdRun(args []string) { cmdnet.Run(appRuntime, args) }
