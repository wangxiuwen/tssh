package main

import cmdnet "github.com/wangxiuwen/tssh/internal/cmd/net"

// cmdBrowser — delegate to internal/cmd/net (Phase 2 net-group extraction).
func cmdBrowser(args []string) { cmdnet.Browser(appRuntime, args) }
