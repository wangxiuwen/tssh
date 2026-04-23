package db

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/wangxiuwen/tssh/internal/aliyun"
	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/model"
	"github.com/wangxiuwen/tssh/internal/session"
	"github.com/wangxiuwen/tssh/internal/shared"
)

// RDS is the package entry point — same pattern as Redis().
func RDS(rt core.Runtime, args []string) {
	appRuntime = rt
	rdsGroup.Dispatch(args)
}

var rdsGroup = shared.CmdGroup{
	Name: "rds",
	Desc: "RDS 实例管理和连接",
	Commands: []shared.SubCmd{
		{Name: "ls", Aliases: []string{"list"}, Desc: "列出 RDS 实例 [-j]", Run: cmdRDSLs},
		{Name: "info", Desc: "RDS 实例详情 <name|id> [-j]", Run: cmdRDSInfo},
	},
}

func cmdRDS(args []string) {
	if len(args) == 0 {
		rdsGroup.PrintHelp()
		os.Exit(1)
		return
	}
	switch args[0] {
	case "ls", "list":
		cmdRDSLs(args[1:])
	case "info":
		cmdRDSInfo(args[1:])
	case "help", "-h", "--help":
		rdsGroup.PrintHelp()
	default:
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

	config := appRuntime.LoadConfig()
	client, err := aliyun.NewRDSClient(config)
	shared.Fatal(err, "create RDS client")

	fmt.Fprintln(os.Stderr, "🔄 正在从阿里云拉取 RDS 实例列表...")
	instances, err := client.FetchAllRDSInstances()
	shared.Fatal(err, "fetch RDS instances")

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

	config := appRuntime.LoadConfig()
	client, err := aliyun.NewRDSClient(config)
	shared.Fatal(err, "create RDS client")

	instances, err := client.FetchAllRDSInstances()
	shared.Fatal(err, "fetch RDS instances")

	var found *model.RDSInstance
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

// cmdRDSConnect connects to an RDS instance via built-in MySQL client
func cmdRDSConnect(args []string) {
	var target, user string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-u") {
			user = strings.TrimPrefix(arg, "-u")
		} else if !strings.HasPrefix(arg, "-") {
			if target == "" {
				target = arg
			}
		}
	}

	if target == "" {
		fmt.Fprintln(os.Stderr, "用法: tssh rds <name|id> [-u<user>]")
		os.Exit(1)
	}
	if user == "" {
		user = "root"
	}

	// 1. Find RDS instance
	config := appRuntime.LoadConfig()
	rdsClient, err := aliyun.NewRDSClient(config)
	shared.Fatal(err, "create RDS client")

	instances, err := rdsClient.FetchAllRDSInstances()
	shared.Fatal(err, "fetch RDS instances")

	var found *model.RDSInstance
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

	// 2. Find an ECS jump host in the same VPC
	ecsInstances := appRuntime.LoadAllInstances()

	var jumpHost *model.Instance
	var fallback *model.Instance
	for i, inst := range ecsInstances {
		if inst.Status != "Running" {
			continue
		}
		if inst.VpcID == found.VpcID && found.VpcID != "" {
			jumpHost = &ecsInstances[i]
			break
		}
		if fallback == nil {
			fallback = &ecsInstances[i]
		}
	}
	if jumpHost == nil && fallback != nil {
		jumpHost = fallback
		fmt.Fprintf(os.Stderr, "⚠️  未找到同 VPC (%s) 的 ECS，使用 %s (VPC: %s)\n", found.VpcID, jumpHost.Name, jumpHost.VpcID)
	}
	if jumpHost == nil {
		fmt.Fprintln(os.Stderr, "❌ 找不到可用的 ECS 实例作为跳板")
		os.Exit(1)
	}

	// 3. Ask for password
	fmt.Fprintf(os.Stderr, "🔗 RDS: %s (%s)\n", found.Name, found.ConnectionString)
	fmt.Fprintf(os.Stderr, "📡 跳板: %s (VPC: %s)\n", jumpHost.Name, jumpHost.VpcID)
	fmt.Fprintf(os.Stderr, "用户: %s\n", user)
	fmt.Fprintf(os.Stderr, "密码: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	password := scanner.Text()

	// 4. Set up socat relay on ECS → RDS
	fmt.Fprintf(os.Stderr, "📡 连接中...\n")

	aliyunClient, err := aliyun.NewClient(config)
	shared.Fatal(err, "create client")

	localPort := 13306
	socatPort := 19901
	host := found.ConnectionString
	// host comes from Aliyun RDS ConnectionString; unlikely to be hostile but
	// shell-quoting it costs nothing and keeps the pattern consistent with
	// connect.go / fwd.go.
	socatCmd := fmt.Sprintf("nohup socat TCP-LISTEN:%d,fork,reuseaddr TCP:'%s':3306 &>/dev/null & echo $!", socatPort, shared.ShellQuote(host))
	result, err := aliyunClient.RunCommand(jumpHost.ID, socatCmd, 10)
	if err != nil {
		fmt.Fprintln(os.Stderr, "⚙️  安装 socat...")
		aliyunClient.RunCommand(jumpHost.ID, "which socat || (apt-get install -y socat 2>/dev/null || yum install -y socat 2>/dev/null)", 30)
		result, err = aliyunClient.RunCommand(jumpHost.ID, socatCmd, 10)
		shared.Fatal(err, "start socat")
	}
	socatPid := strings.TrimSpace(shared.DecodeOutput(result.Output))

	defer func() {
		if socatPid != "" {
			aliyunClient.RunCommand(jumpHost.ID, fmt.Sprintf("kill %s 2>/dev/null", socatPid), 5)
		}
	}()

	// 5. Start local port forward to ECS socat port
	stop, err := session.StartPortForwardBgWithCancel(config, jumpHost.ID, localPort, socatPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 端口转发失败: %v\n", err)
		os.Exit(1)
	}
	defer stop()

	// 6. Connect built-in MySQL REPL to local forwarded port
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 连接失败: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	mysqlRepl(conn, user, password)
}
