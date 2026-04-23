package main

import (
	"sync"

	"github.com/wangxiuwen/tssh/internal/aliyun"
	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/model"
	tssruntime "github.com/wangxiuwen/tssh/internal/runtime"
)

// appRuntime — lazy-initialised so globalProfile (parsed in main() from
// --profile) is populated before we bind it into the runtime. Package-init
// time is too early: at that point globalProfile is still "".
//
// We build on internal/runtime (shared with tssh-k8s) and inject hooks for
// interactive session + port-forward + socat relay, which still live in
// cmd/tssh. As those move to internal packages the hooks shrink and slim
// binaries inherit the capability for free.
var (
	appRuntimeOnce sync.Once
	appRuntimeVal  core.Runtime
)

// appRuntimeProxy defers real construction until first use so main() has
// time to parse --profile. Implements core.Runtime by pass-through.
type appRuntimeProxy struct{}

func (appRuntimeProxy) LoadConfig() *model.Config      { return realAppRuntime().LoadConfig() }
func (appRuntimeProxy) ResolveInstance(n string) *model.Instance {
	return realAppRuntime().ResolveInstance(n)
}
func (appRuntimeProxy) LoadAllInstances() []model.Instance { return realAppRuntime().LoadAllInstances() }
func (appRuntimeProxy) ExecOneShot(id, c string, t int) (*core.ExecResult, error) {
	return realAppRuntime().ExecOneShot(id, c, t)
}
func (appRuntimeProxy) ExecInteractive(id, c string) error {
	return realAppRuntime().ExecInteractive(id, c)
}
func (appRuntimeProxy) StartPortForward(id string, lp, rp int) (func(), error) {
	return realAppRuntime().StartPortForward(id, lp, rp)
}
func (appRuntimeProxy) StartSocatRelay(j, h string, p int) (int, func(), error) {
	return realAppRuntime().StartSocatRelay(j, h, p)
}

var appRuntime core.Runtime = appRuntimeProxy{}

func realAppRuntime() core.Runtime {
	appRuntimeOnce.Do(func() {
		rt := tssruntime.New(globalProfile)
		rt.ExecInteractiveFn = func(cfg *model.Config, id, cmd string) error {
			return ConnectSessionWithCommand(cfg, id, cmd)
		}
		rt.StartPortForwardFn = func(cfg *model.Config, id string, lp, rp int) (func(), error) {
			return startPortForwardBgWithCancel(cfg, id, lp, rp)
		}
		rt.StartSocatRelayFn = func(c *aliyun.Client, jumpID, host string, port int) (int, func(), error) {
			socat, _, cleanup, err := setupSocatRelay(c, jumpID, host, port)
			return socat, cleanup, err
		}
		appRuntimeVal = rt
	})
	return appRuntimeVal
}
