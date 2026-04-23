package main

import cmdnet "github.com/wangxiuwen/tssh/internal/cmd/net"

// cmdShell — delegate to internal/cmd/net (Phase 2 net-group extraction).
func cmdShell(args []string) { cmdnet.Shell(appRuntime, args) }
