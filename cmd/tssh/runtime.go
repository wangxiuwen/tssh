package main

import (
	"fmt"
	"os"

	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/model"
)

// tsshRuntime is the cmd/tssh-side implementation of core.Runtime. It wires
// the legacy package-level helpers (mustLoadConfig / resolveInstance /
// getCache / NewAliyunClient) into the interface that internal/cmd/* groups
// depend on. Groups stay decoupled from cmd/tssh — when we add tssh-k8s et
// al., each one will build its own concrete runtime the same way.
type tsshRuntime struct{}

// appRuntime is the process-wide core.Runtime. Groups (when they get wired
// up) read this to do config/instance/exec work. Named appRuntime because
// plain "runtime" collides with the Go stdlib package of that name, which
// several of our files (vpn/browser/info/...) already import.
var appRuntime core.Runtime = &tsshRuntime{}

func (r *tsshRuntime) LoadConfig() *model.Config {
	return mustLoadConfig()
}

func (r *tsshRuntime) ResolveInstance(name string) *model.Instance {
	c := getCache()
	inst, err := resolveInstance(c, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		return nil
	}
	return inst
}

func (r *tsshRuntime) LoadAllInstances() []model.Instance {
	c := getCache()
	ensureCache(c)
	insts, err := c.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ load cache: %v\n", err)
		return nil
	}
	return insts
}

func (r *tsshRuntime) ExecOneShot(instanceID, cmd string, timeoutSec int) (*core.ExecResult, error) {
	client, err := NewAliyunClient(mustLoadConfig())
	if err != nil {
		return nil, err
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	res, err := client.RunCommand(instanceID, cmd, timeoutSec)
	if err != nil {
		return nil, err
	}
	return &core.ExecResult{
		Output:   decodeOutput(res.Output),
		ExitCode: res.ExitCode,
	}, nil
}

func (r *tsshRuntime) ExecInteractive(instanceID, cmd string) error {
	return ConnectSessionWithCommand(mustLoadConfig(), instanceID, cmd)
}

func (r *tsshRuntime) StartPortForward(instanceID string, localPort, remotePort int) (func(), error) {
	return startPortForwardBgWithCancel(mustLoadConfig(), instanceID, localPort, remotePort)
}

func (r *tsshRuntime) StartSocatRelay(jumpID, remoteHost string, remotePort int) (int, func(), error) {
	client, err := NewAliyunClient(mustLoadConfig())
	if err != nil {
		return 0, nil, err
	}
	socatPort, _, cleanup, err := setupSocatRelay(client, jumpID, remoteHost, remotePort)
	return socatPort, cleanup, err
}
