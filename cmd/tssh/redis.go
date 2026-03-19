package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// cmdRedis routes redis subcommands
func cmdRedis(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: tssh redis <ls|info> [options]")
		os.Exit(1)
	}
	switch args[0] {
	case "ls", "list":
		cmdRedisLs(args[1:])
	case "info":
		cmdRedisInfo(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: redis %s\n", args[0])
		os.Exit(1)
	}
}

// cmdRedisLs lists all Redis instances
func cmdRedisLs(args []string) {
	jsonMode := false
	for _, arg := range args {
		if arg == "-j" || arg == "--json" {
			jsonMode = true
		}
	}

	config := mustLoadConfig()
	client, err := NewRedisClient(config)
	fatal(err, "create Redis client")

	fmt.Fprintln(os.Stderr, "🔄 正在从阿里云拉取 Redis 实例列表...")
	instances, err := client.FetchAllRedisInstances()
	fatal(err, "fetch Redis instances")

	if jsonMode {
		data, _ := json.Marshal(instances)
		fmt.Println(string(data))
		return
	}

	if len(instances) == 0 {
		fmt.Println("没有找到 Redis 实例")
		return
	}

	fmt.Printf("%-4s  %-22s  %-20s  %-8s  %-8s  %-6s  %-36s  %-5s\n",
		"#", "ID", "名称", "状态", "版本", "容量MB", "连接地址", "端口")
	fmt.Println(strings.Repeat("─", 140))
	for i, inst := range instances {
		fmt.Printf("%-4d  %-22s  %-20s  %-8s  %-8s  %-6d  %-36s  %-5d\n",
			i+1,
			inst.ID,
			truncateStr(inst.Name, 20),
			inst.Status,
			inst.EngineVersion,
			inst.Capacity,
			truncateStr(inst.ConnectionDomain, 36),
			inst.Port,
		)
	}
	fmt.Fprintf(os.Stderr, "\n共 %d 个 Redis 实例\n", len(instances))
}

// cmdRedisInfo shows detailed information about a Redis instance
func cmdRedisInfo(args []string) {
	jsonMode := false
	var target string
	for _, arg := range args {
		if arg == "-j" || arg == "--json" {
			jsonMode = true
		} else {
			target = arg
		}
	}

	if target == "" {
		fmt.Fprintln(os.Stderr, "用法: tssh redis info <name|id> [-j]")
		os.Exit(1)
	}

	config := mustLoadConfig()
	client, err := NewRedisClient(config)
	fatal(err, "create Redis client")

	instances, err := client.FetchAllRedisInstances()
	fatal(err, "fetch Redis instances")

	var found *RedisInstance
	target = strings.ToLower(target)
	for i, inst := range instances {
		if strings.ToLower(inst.ID) == target || strings.ToLower(inst.Name) == target {
			found = &instances[i]
			break
		}
	}
	// Partial match
	if found == nil {
		for i, inst := range instances {
			if strings.Contains(strings.ToLower(inst.Name), target) ||
				strings.Contains(strings.ToLower(inst.ID), target) {
				found = &instances[i]
				break
			}
		}
	}

	if found == nil {
		fmt.Fprintf(os.Stderr, "❌ 找不到 Redis 实例: %s\n", target)
		os.Exit(1)
	}

	if jsonMode {
		data, _ := json.MarshalIndent(found, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Printf("📋 Redis 实例详情\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("  实例 ID:    %s\n", found.ID)
	fmt.Printf("  名称:      %s\n", found.Name)
	fmt.Printf("  状态:      %s\n", found.Status)
	fmt.Printf("  类型:      %s\n", found.InstanceType)
	fmt.Printf("  架构:      %s\n", found.ArchitectureType)
	fmt.Printf("  规格:      %s\n", found.InstanceClass)
	fmt.Printf("  引擎版本:  Redis %s\n", found.EngineVersion)
	fmt.Printf("  容量:      %d MB\n", found.Capacity)
	fmt.Printf("  连接地址:  %s:%d\n", found.ConnectionDomain, found.Port)
	fmt.Printf("  内网 IP:   %s\n", found.PrivateIP)
	fmt.Printf("  网络类型:  %s\n", found.NetworkType)
	fmt.Printf("  VPC:       %s\n", found.VpcID)
	fmt.Printf("  区域:      %s\n", found.RegionID)
	fmt.Printf("  可用区:    %s\n", found.ZoneID)
	fmt.Printf("  付费类型:  %s\n", found.ChargeType)
	fmt.Printf("  创建时间:  %s\n", found.CreateTime)
	fmt.Printf("  到期时间:  %s\n", found.EndTime)
	fmt.Printf("  最大连接:  %d\n", found.Connections)
	fmt.Printf("  带宽:      %d Mbps\n", found.Bandwidth)
	fmt.Printf("  QPS:       %d\n", found.QPS)
}
