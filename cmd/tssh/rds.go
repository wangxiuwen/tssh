package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// cmdRDS routes rds subcommands
func cmdRDS(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: tssh rds <ls|info> [options]")
		os.Exit(1)
	}
	switch args[0] {
	case "ls", "list":
		cmdRDSLs(args[1:])
	case "info":
		cmdRDSInfo(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: rds %s\n", args[0])
		os.Exit(1)
	}
}

// cmdRDSLs lists all RDS instances
func cmdRDSLs(args []string) {
	jsonMode := false
	for _, arg := range args {
		if arg == "-j" || arg == "--json" {
			jsonMode = true
		}
	}

	config := mustLoadConfig()
	client, err := NewRDSClient(config)
	fatal(err, "create RDS client")

	fmt.Fprintln(os.Stderr, "🔄 正在从阿里云拉取 RDS 实例列表...")
	instances, err := client.FetchAllRDSInstances()
	fatal(err, "fetch RDS instances")

	if jsonMode {
		data, _ := json.Marshal(instances)
		fmt.Println(string(data))
		return
	}

	if len(instances) == 0 {
		fmt.Println("没有找到 RDS 实例")
		return
	}

	fmt.Printf("%-4s  %-22s  %-20s  %-8s  %-10s  %-6s  %-16s  %-40s\n",
		"#", "ID", "名称", "状态", "引擎", "版本", "规格", "连接地址")
	fmt.Println(strings.Repeat("─", 140))
	for i, inst := range instances {
		engine := inst.Engine
		if inst.EngineVersion != "" {
			engine = inst.Engine + " " + inst.EngineVersion
		}
		fmt.Printf("%-4d  %-22s  %-20s  %-8s  %-10s  %-6s  %-16s  %-40s\n",
			i+1,
			inst.ID,
			truncateStr(inst.Name, 20),
			inst.Status,
			truncateStr(engine, 10),
			inst.EngineVersion,
			truncateStr(inst.InstanceClass, 16),
			truncateStr(inst.ConnectionString, 40),
		)
	}
	fmt.Fprintf(os.Stderr, "\n共 %d 个 RDS 实例\n", len(instances))
}

// cmdRDSInfo shows detailed information about an RDS instance
func cmdRDSInfo(args []string) {
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
		fmt.Fprintln(os.Stderr, "用法: tssh rds info <name|id> [-j]")
		os.Exit(1)
	}

	config := mustLoadConfig()
	client, err := NewRDSClient(config)
	fatal(err, "create RDS client")

	instances, err := client.FetchAllRDSInstances()
	fatal(err, "fetch RDS instances")

	var found *RDSInstance
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
		fmt.Fprintf(os.Stderr, "❌ 找不到 RDS 实例: %s\n", target)
		os.Exit(1)
	}

	if jsonMode {
		data, _ := json.MarshalIndent(found, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Printf("📋 RDS 实例详情\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("  实例 ID:    %s\n", found.ID)
	fmt.Printf("  名称:      %s\n", found.Name)
	fmt.Printf("  状态:      %s\n", found.Status)
	fmt.Printf("  引擎:      %s %s\n", found.Engine, found.EngineVersion)
	fmt.Printf("  规格:      %s\n", found.InstanceClass)
	fmt.Printf("  类别:      %s\n", found.Category)
	fmt.Printf("  连接地址:  %s\n", found.ConnectionString)
	fmt.Printf("  网络类型:  %s\n", found.NetworkType)
	fmt.Printf("  VPC:       %s\n", found.VpcID)
	fmt.Printf("  区域:      %s\n", found.RegionID)
	fmt.Printf("  可用区:    %s\n", found.ZoneID)
	fmt.Printf("  付费类型:  %s\n", found.PayType)
	fmt.Printf("  锁定状态:  %s\n", found.LockMode)
	fmt.Printf("  创建时间:  %s\n", found.CreateTime)
	fmt.Printf("  到期时间:  %s\n", found.ExpireTime)
}
