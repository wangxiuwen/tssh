package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
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
		// Treat as connect: tssh rds <name>
		cmdRDSConnect(args)
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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "#\tID\t名称\t状态\t引擎\t规格\t连接地址\n")
	for i, inst := range instances {
		engine := inst.Engine
		if inst.EngineVersion != "" {
			engine = inst.Engine + " " + inst.EngineVersion
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1,
			inst.ID,
			inst.Name,
			inst.Status,
			engine,
			inst.InstanceClass,
			inst.ConnectionString,
		)
	}
	w.Flush()
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

// cmdRDSConnect connects to an RDS instance via ECS terminal session
func cmdRDSConnect(args []string) {
	var target string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			target = arg
			break
		}
	}

	if target == "" {
		fmt.Fprintln(os.Stderr, "用法: tssh rds <name|id>")
		os.Exit(1)
	}

	// 1. Find RDS instance
	config := mustLoadConfig()
	rdsClient, err := NewRDSClient(config)
	fatal(err, "create RDS client")

	instances, err := rdsClient.FetchAllRDSInstances()
	fatal(err, "fetch RDS instances")

	var found *RDSInstance
	targetLower := strings.ToLower(target)
	for i, inst := range instances {
		if strings.ToLower(inst.ID) == targetLower || strings.ToLower(inst.Name) == targetLower {
			found = &instances[i]
			break
		}
	}
	if found == nil {
		for i, inst := range instances {
			if strings.Contains(strings.ToLower(inst.Name), targetLower) ||
				strings.Contains(strings.ToLower(inst.ID), targetLower) {
				found = &instances[i]
				break
			}
		}
	}
	if found == nil {
		fmt.Fprintf(os.Stderr, "❌ 找不到 RDS 实例: %s\n", target)
		os.Exit(1)
	}

	// 2. Find an ECS jump host
	cache := getCache()
	ensureCache(cache)
	ecsInstances, err := cache.Load()
	fatal(err, "load ECS cache")

	var jumpHost *Instance
	for i, inst := range ecsInstances {
		if inst.Status == "Running" {
			jumpHost = &ecsInstances[i]
			break
		}
	}
	if jumpHost == nil {
		fmt.Fprintln(os.Stderr, "❌ 找不到可用的 ECS 实例作为跳板")
		os.Exit(1)
	}

	// 3. Connect to ECS and run mysql
	host := found.ConnectionString
	fmt.Fprintf(os.Stderr, "🔗 RDS: %s (%s)\n", found.Name, host)
	fmt.Fprintf(os.Stderr, "📡 通过 ECS %s 连接...\n", jumpHost.Name)

	cmd := fmt.Sprintf("mysql -h %s -u root -p", host)
	err = ConnectSessionWithCommand(config, jumpHost.ID, cmd)
	fatal(err, "connect")
}
