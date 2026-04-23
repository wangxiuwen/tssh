package main

import (
	cmdnet "github.com/wangxiuwen/tssh/internal/cmd/net"
	"github.com/wangxiuwen/tssh/internal/session"
	"github.com/wangxiuwen/tssh/internal/shared"
)

// cmdFwd — delegate to internal/cmd/net (Phase 2 net-group extraction).
func cmdFwd(args []string) { cmdnet.Fwd(appRuntime, args) }

// resolveFwdTarget / pickJumpHost — thin wrappers over the net-group helpers.
// Kept because cmd/tssh/run.go still uses the lowercase names. Those call
// sites migrate when run.go moves to the net group.
func resolveFwdTarget(_ *Config, target string) (host string, port int, vpcID string, err error) {
	return cmdnet.ResolveFwdTarget(appRuntime, target)
}

func pickJumpHost(_ *Cache, vpcID, override string) (*Instance, error) {
	return cmdnet.PickJumpHost(appRuntime, vpcID, override)
}

func setupSocatRelay(client *AliyunClient, jumpID, remoteHost string, remotePort int) (int, string, func(), error) {
	return session.SetupSocatRelay(client, jumpID, remoteHost, remotePort)
}

func findFreePortInRange(start, end int) int {
	return shared.FindFreePortInRange(start, end)
}
