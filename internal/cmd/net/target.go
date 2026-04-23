package net

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wangxiuwen/tssh/internal/aliyun"
	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/model"
)

// Target resolution + jump-host selection helpers shared by fwd and run.
// Kept package-private because they aren't part of the runtime contract —
// they're pure net-group bookkeeping.

// ResolveFwdTarget expands a tssh fwd target into host:port + optional VPC.
// RDS/Redis IDs need an API call; vpcID is "" for raw host:port (pickJumpHost
// will then fall back to the first Running ECS).
func ResolveFwdTarget(rt core.Runtime, target string) (host string, port int, vpcID string, err error) {
	switch {
	case strings.HasPrefix(target, "rm-"):
		return resolveRDSTarget(rt, target)
	case strings.HasPrefix(target, "r-"):
		return resolveRedisTarget(rt, target)
	}
	// host:port
	idx := strings.LastIndex(target, ":")
	if idx <= 0 || idx == len(target)-1 {
		return "", 0, "", fmt.Errorf("target 格式: host:port / rm-xxx / r-xxx, 收到: %q", target)
	}
	p, perr := strconv.Atoi(target[idx+1:])
	if perr != nil || p <= 0 || p > 65535 {
		return "", 0, "", fmt.Errorf("invalid port in %q", target)
	}
	return target[:idx], p, "", nil
}

func resolveRDSTarget(rt core.Runtime, id string) (string, int, string, error) {
	client, err := aliyun.NewRDSClient(rt.LoadConfig())
	if err != nil {
		return "", 0, "", err
	}
	insts, err := client.FetchAllRDSInstances()
	if err != nil {
		return "", 0, "", err
	}
	for _, inst := range insts {
		if inst.ID != id {
			continue
		}
		// RDS ConnectionString rarely embeds a port; default by engine.
		p := 3306
		if strings.Contains(strings.ToLower(inst.Engine), "postgres") {
			p = 5432
		} else if strings.Contains(strings.ToLower(inst.Engine), "sqlserver") {
			p = 1433
		}
		return inst.ConnectionString, p, inst.VpcID, nil
	}
	return "", 0, "", fmt.Errorf("RDS 实例不存在: %s", id)
}

func resolveRedisTarget(rt core.Runtime, id string) (string, int, string, error) {
	client, err := aliyun.NewRedisClient(rt.LoadConfig())
	if err != nil {
		return "", 0, "", err
	}
	insts, err := client.FetchAllRedisInstances()
	if err != nil {
		return "", 0, "", err
	}
	for _, inst := range insts {
		if inst.ID != id {
			continue
		}
		p := int(inst.Port)
		if p == 0 {
			p = 6379
		}
		return inst.ConnectionDomain, p, inst.VpcID, nil
	}
	return "", 0, "", fmt.Errorf("Redis 实例不存在: %s", id)
}

// PickJumpHost chooses an ECS to relay through.
//
//  1. --via override (resolved via runtime)
//  2. Any Running instance in the target's VPC
//  3. Any Running instance at all (with a warning)
func PickJumpHost(rt core.Runtime, vpcID, override string) (*model.Instance, error) {
	if override != "" {
		inst := rt.ResolveInstance(override)
		if inst == nil {
			return nil, fmt.Errorf("--via %s: not found", override)
		}
		return inst, nil
	}

	insts := rt.LoadAllInstances()
	var fallback *model.Instance
	for i := range insts {
		if insts[i].Status != "Running" {
			continue
		}
		if vpcID != "" && insts[i].VpcID == vpcID {
			return &insts[i], nil
		}
		if fallback == nil {
			fallback = &insts[i]
		}
	}
	if fallback == nil {
		return nil, fmt.Errorf("没有 Running 状态的 ECS 可用作跳板")
	}
	if vpcID != "" && fallback.VpcID != vpcID {
		fmt.Fprintf(os.Stderr, "⚠️  未找到同 VPC (%s) 的 ECS, 使用 %s (VPC: %s) — 跨 VPC 可能不通\n",
			vpcID, fallback.Name, fallback.VpcID)
	}
	return fallback, nil
}
