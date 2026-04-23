package main

import (
	cmdnet "github.com/wangxiuwen/tssh/internal/cmd/net"
	"github.com/wangxiuwen/tssh/internal/session"
)

// cmdSocks — delegate to internal/cmd/net (Phase 2 net-group extraction).
func cmdSocks(args []string) { cmdnet.Socks(appRuntime, args) }

// startRemoteSocks — wrapper so existing cmd/tssh callers (browser.go,
// shell.go, vpn.go) keep compiling until they migrate to the net group.
func startRemoteSocks(client *AliyunClient, instanceID string, port int) (string, error) {
	return session.StartRemoteSocks(client, instanceID, port)
}
