package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"text/tabwriter"
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
		// Treat as connect: tssh redis <name>
		cmdRedisConnect(args)
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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "#\tID\t名称\t状态\t版本\t容量MB\t连接地址\t端口\n")
	for i, inst := range instances {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%d\t%s\t%d\n",
			i+1,
			inst.ID,
			inst.Name,
			inst.Status,
			inst.EngineVersion,
			inst.Capacity,
			inst.ConnectionDomain,
			inst.Port,
		)
	}
	w.Flush()
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

// cmdRedisConnect connects to a Redis instance via built-in RESP client
func cmdRedisConnect(args []string) {
	var target string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			target = arg
			break
		}
	}

	if target == "" {
		fmt.Fprintln(os.Stderr, "用法: tssh redis <name|id>")
		os.Exit(1)
	}

	// 1. Find Redis instance
	config := mustLoadConfig()
	redisClient, err := NewRedisClient(config)
	fatal(err, "create Redis client")

	instances, err := redisClient.FetchAllRedisInstances()
	fatal(err, "fetch Redis instances")

	var found *RedisInstance
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
		fmt.Fprintf(os.Stderr, "❌ 找不到 Redis 实例: %s\n", target)
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

	// 3. Set up socat relay on ECS → Redis
	remotePort := int(found.Port)
	if remotePort == 0 {
		remotePort = 6379
	}
	localPort := 16379

	fmt.Fprintf(os.Stderr, "🔗 Redis: %s (%s:%d)\n", found.Name, found.ConnectionDomain, remotePort)
	fmt.Fprintf(os.Stderr, "📡 通过 ECS %s 中转...\n", jumpHost.Name)

	aliyunClient, err := NewAliyunClient(config)
	fatal(err, "create client")

	socatPort := 19900
	socatCmd := fmt.Sprintf("nohup socat TCP-LISTEN:%d,fork,reuseaddr TCP:%s:%d &>/dev/null & echo $!", socatPort, found.ConnectionDomain, remotePort)
	result, err := aliyunClient.RunCommand(jumpHost.ID, socatCmd, 10)
	if err != nil {
		fmt.Fprintln(os.Stderr, "⚙️  安装 socat...")
		aliyunClient.RunCommand(jumpHost.ID, "which socat || (apt-get install -y socat 2>/dev/null || yum install -y socat 2>/dev/null)", 30)
		result, err = aliyunClient.RunCommand(jumpHost.ID, socatCmd, 10)
		fatal(err, "start socat")
	}
	socatPid := strings.TrimSpace(decodeOutput(result.Output))

	defer func() {
		if socatPid != "" {
			aliyunClient.RunCommand(jumpHost.ID, fmt.Sprintf("kill %s 2>/dev/null", socatPid), 5)
		}
	}()

	// 4. Start local port forward to ECS socat port
	stop, err := startPortForwardBgWithCancel(config, jumpHost.ID, localPort, socatPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 端口转发失败: %v\n", err)
		os.Exit(1)
	}
	defer stop()

	// 5. Connect built-in Redis REPL to local forwarded port
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 连接失败: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	redisRepl(conn)
}
