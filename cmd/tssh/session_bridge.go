package main

import (
	"github.com/wangxiuwen/tssh/internal/model"
	"github.com/wangxiuwen/tssh/internal/session"
)

// Thin delegates to internal/session. Kept because a lot of legacy cmd/tssh
// call sites still use the lowercase names; updating each is unnecessary as
// long as the shim costs are zero and the implementation lives in one place.

func ConnectSession(cfg *model.Config, instanceID string) error {
	return session.ConnectSession(cfg, instanceID)
}

func ConnectSessionWithCommand(cfg *model.Config, instanceID, command string) error {
	return session.ConnectSessionWithCommand(cfg, instanceID, command)
}

func PortForward(cfg *model.Config, instanceID string, lp, rp int) error {
	return session.PortForward(cfg, instanceID, lp, rp)
}

func startPortForwardBgWithCancel(cfg *model.Config, instanceID string, lp, rp int) (func(), error) {
	return session.StartPortForwardBgWithCancel(cfg, instanceID, lp, rp)
}

func startNativePortForward(cfg *model.Config, instanceID string, lp, rp int) error {
	return session.StartNativePortForward(cfg, instanceID, lp, rp)
}
