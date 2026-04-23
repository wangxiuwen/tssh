package main

import cmdnet "github.com/wangxiuwen/tssh/internal/cmd/net"

// cmdVPN — delegate to internal/cmd/net (Phase 2 net-group extraction).
func cmdVPN(args []string) { cmdnet.VPN(appRuntime, args) }
