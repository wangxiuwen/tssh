// Package runtime is the concrete core.Runtime implementation shared by the
// main tssh binary and the per-group slim binaries (tssh-k8s, tssh-net, ...).
//
// Scope this iteration (Phase 3 of the split): enough surface to run the
// k8s group's ks / logs / events end-to-end. kf (needs port-forward +
// socat relay), net group, and anything interactive still route through the
// cmd/tssh-embedded runtime until a later pass moves session.go /
// portforward.go / fwd.go's socat helper into this package too.
package runtime

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wangxiuwen/tssh/internal/aliyun"
	"github.com/wangxiuwen/tssh/internal/cache"
	"github.com/wangxiuwen/tssh/internal/config"
	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/model"
	"github.com/wangxiuwen/tssh/internal/session"
)

// Runtime wires up just enough of the tssh internals to satisfy core.Runtime
// from a standalone binary. Profile-scoped: cache and config both honor the
// active profile. Passed-in execInteractive and startPortForward / startSocat
// hooks let the full tssh binary inject the richer session.go-based versions
// while smaller binaries get either nil (fail politely) or a future move of
// those helpers into this package.
type Runtime struct {
	profile string

	// Optional hooks for capabilities not yet living in this package.
	// tssh (the full binary) wires the session.go / fwd.go versions; the
	// slim binaries leave them nil for now and get a "not available yet"
	// error when a subcommand reaches for them.
	ExecInteractiveFn   func(cfg *model.Config, instanceID, cmd string) error
	StartPortForwardFn  func(cfg *model.Config, instanceID string, lp, rp int) (func(), error)
	StartSocatRelayFn   func(client *aliyun.Client, jumpID, host string, port int) (int, func(), error)
}

// New builds a Runtime bound to the given profile. profile="" means "use
// whatever LoadConfig picks" (env vars, default profile in config.json).
//
// Defaults are wired to internal/session's PortForward + ConnectSessionWithCommand
// so every binary gets these capabilities for free. Callers can still
// override the Fn fields (e.g. cmd/tssh keeps the same session functions
// but could swap in a no-op for a test binary).
func New(profile string) *Runtime {
	return &Runtime{
		profile:            profile,
		ExecInteractiveFn:  session.ConnectSessionWithCommand,
		StartPortForwardFn: session.StartPortForwardBgWithCancel,
		StartSocatRelayFn: func(c *aliyun.Client, jumpID, host string, port int) (int, func(), error) {
			socat, _, cleanup, err := session.SetupSocatRelay(c, jumpID, host, port)
			return socat, cleanup, err
		},
	}
}

// ---- core.Runtime impl ----

func (r *Runtime) LoadConfig() *model.Config {
	cfg, err := config.Load(r.profile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ load config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func (r *Runtime) newCache() *cache.Cache {
	if r.profile != "" {
		return cache.NewWithProfile(r.profile)
	}
	return cache.New()
}

func (r *Runtime) LoadAllInstances() []model.Instance {
	c := r.newCache()
	r.ensureCache(c)
	insts, err := c.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ load cache: %v\n", err)
		return nil
	}
	return insts
}

// ensureCache syncs the cache from Aliyun when it doesn't exist, and warns
// (non-blocking) when it's stale. No background refresh here (cmd/tssh does
// that) — simpler semantics for batch-style slim binaries.
func (r *Runtime) ensureCache(c *cache.Cache) {
	if c.Exists() {
		// 7 days matches the unattended-use threshold: slim binaries don't
		// auto-refresh, so users relying only on tssh-k8s/net/etc. might miss
		// new/renamed instances for a long time. Warn but don't block.
		if age := c.Age(); age > 7*24*time.Hour {
			fmt.Fprintf(os.Stderr, "⚠️  缓存 %s 未更新, 可能缺少新实例 — 跑 `tssh sync` 刷新\n",
				age.Round(time.Hour))
		}
		return
	}
	fmt.Fprintln(os.Stderr, "⚠️  缓存不存在, 正在同步...")
	cfg := r.LoadConfig()
	client, err := aliyun.NewClient(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ aliyun client: %v\n", err)
		os.Exit(1)
	}
	insts, err := client.FetchAllInstances()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ fetch instances: %v\n", err)
		os.Exit(1)
	}
	_ = c.Save(insts)
}

// ResolveInstance does a NON-TUI resolution: tries name / ID exact match,
// then index, then pattern (first match). The full tssh binary still has
// FuzzySelect for interactive use; slim binaries skip that to avoid pulling
// in the TUI deps.
func (r *Runtime) ResolveInstance(name string) *model.Instance {
	c := r.newCache()
	r.ensureCache(c)
	insts, err := c.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ load cache: %v\n", err)
		return nil
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "❌ 需要实例名 (slim runtime 不支持交互选择, 请显式传名)")
		return nil
	}
	// Index: "tssh 5"
	if idx, err := strconv.Atoi(name); err == nil && idx >= 1 && idx <= len(insts) {
		return &insts[idx-1]
	}
	// Exact by name then by id
	lower := strings.ToLower(name)
	for i := range insts {
		if strings.ToLower(insts[i].Name) == lower || strings.ToLower(insts[i].ID) == lower {
			return &insts[i]
		}
	}
	// Pattern match → first hit
	matches := cache.FilterInstances(insts, name)
	if len(matches) >= 1 {
		return &matches[0]
	}
	fmt.Fprintf(os.Stderr, "❌ 找不到 %q\n", name)
	return nil
}

func (r *Runtime) ExecOneShot(instanceID, cmd string, timeoutSec int) (*core.ExecResult, error) {
	client, err := aliyun.NewClient(r.LoadConfig())
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
	// shared.DecodeOutput handles base64 and falls back on plain text — do the
	// decode here so groups see ready-to-print strings.
	return &core.ExecResult{
		Output:   decodeBase64OrPassThrough(res.Output),
		ExitCode: res.ExitCode,
	}, nil
}

func (r *Runtime) ExecInteractive(instanceID, cmd string) error {
	if r.ExecInteractiveFn == nil {
		return fmt.Errorf("interactive session not wired in this binary (slim runtime) — 用主 tssh 二进制")
	}
	return r.ExecInteractiveFn(r.LoadConfig(), instanceID, cmd)
}

func (r *Runtime) StartPortForward(instanceID string, localPort, remotePort int) (func(), error) {
	if r.StartPortForwardFn == nil {
		return nil, fmt.Errorf("port-forward not wired in this binary (slim runtime) — 用主 tssh 二进制")
	}
	return r.StartPortForwardFn(r.LoadConfig(), instanceID, localPort, remotePort)
}

func (r *Runtime) StartSocatRelay(jumpID, remoteHost string, remotePort int) (int, func(), error) {
	if r.StartSocatRelayFn == nil {
		return 0, nil, fmt.Errorf("socat relay not wired (slim runtime)")
	}
	client, err := aliyun.NewClient(r.LoadConfig())
	if err != nil {
		return 0, nil, err
	}
	return r.StartSocatRelayFn(client, jumpID, remoteHost, remotePort)
}

// decodeBase64OrPassThrough — duplicated of shared.DecodeOutput but kept
// local so this package can stay independent of shared (avoid import cycles
// once shared grows). Pure logic, trivial to keep in sync.
func decodeBase64OrPassThrough(s string) string {
	// Leverage shared via a tiny indirection wouldn't simplify — inline the
	// whole 4-line function.
	return callDecodeOutput(s)
}
