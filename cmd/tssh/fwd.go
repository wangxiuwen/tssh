package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

// cmdFwd is the zero-config "make me a local port to this service" command.
// Accepts host:port, RDS instance IDs (rm-...), or Redis instance IDs (r-...)
// and auto-picks a same-VPC ECS as the jump host. A single short line covers
// most "dev needs to hit prod MySQL right now" situations.
//
//	tssh fwd rds-prod.internal:3306
//	tssh fwd 10.0.0.5:8080
//	tssh fwd rm-2zxxxxxx
//	tssh fwd r-bpxxxxxx --local 6380
//	tssh fwd 10.0.0.5:8080 --via jump-prod
func cmdFwd(args []string) {
	var target, via string
	var localPort int
	var jsonMode, quietMode bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-j", "--json":
			jsonMode = true
		case "-q", "--quiet":
			quietMode = true
		case "--via":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "❌ --via 需要一个 name/id")
				os.Exit(2)
			}
			via = args[i+1]
			i++
		case "--local", "-p":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "❌ --local 需要一个端口号")
				os.Exit(2)
			}
			p, err := strconv.Atoi(args[i+1])
			if err != nil || p <= 0 || p > 65535 {
				fmt.Fprintf(os.Stderr, "❌ 无效本地端口: %s\n", args[i+1])
				os.Exit(2)
			}
			localPort = p
			i++
		case "-h", "--help":
			printFwdHelp()
			return
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "❌ 未知选项: %s\n", args[i])
				os.Exit(2)
			}
			if target != "" {
				fmt.Fprintln(os.Stderr, "❌ 只能指定一个 target")
				os.Exit(2)
			}
			target = args[i]
		}
	}

	if target == "" {
		printFwdHelp()
		os.Exit(1)
	}

	cfg := mustLoadConfig()
	host, port, vpcID, err := resolveFwdTarget(cfg, target)
	fatal(err, "resolve target")

	cache := getCache()
	ensureCache(cache)
	jumpHost, err := pickJumpHost(cache, vpcID, via)
	fatal(err, "pick jump host")

	if localPort == 0 {
		localPort = findFreePort()
	}

	client, err := NewAliyunClient(cfg)
	fatal(err, "create client")

	var socatPort int
	var cleanup func()
	if host == "localhost" || host == "127.0.0.1" {
		// The target already lives on the jump host; no relay needed.
		socatPort = port
	} else {
		socatPort, _, cleanup, err = setupSocatRelay(client, jumpHost.ID, host, port)
		fatal(err, "setup socat relay")
	}
	if cleanup != nil {
		defer cleanup()
	}

	stop, err := startPortForwardBgWithCancel(cfg, jumpHost.ID, localPort, socatPort)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		fatal(err, "portforward")
	}
	defer stop()

	// JSON mode: one line on stdout, structured so AI agents / scripts can
	// parse it directly and start using local_port. Human mode keeps the
	// emoji-rich layout on stderr.
	if jsonMode {
		payload := map[string]interface{}{
			"local_port":  localPort,
			"host":        host,
			"remote_port": port,
			"jump":        jumpHost.Name,
			"jump_id":     jumpHost.ID,
			"pid":         os.Getpid(),
		}
		b, _ := json.Marshal(payload)
		fmt.Println(string(b))
		// Flush stdout so `tssh fwd -j &` consumers can readline right away.
		os.Stdout.Sync()
	} else if !quietMode {
		fmt.Println()
		fmt.Printf("📡 127.0.0.1:%d  →  %s:%d  (via %s)\n", localPort, host, port, jumpHost.Name)
		fmt.Println()
		fmt.Println("按 Ctrl+C 退出")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	if !quietMode && !jsonMode {
		fmt.Fprintln(os.Stderr, "\n🛑 清理中...")
	}
}

// resolveFwdTarget expands a tssh fwd target into host:port + optional VPC.
// RDS/Redis IDs need an API call, which is why this takes a *Config.
// Returns vpcID="" when the target is a raw host:port (pickJumpHost will then
// fall back to the first Running ECS).
func resolveFwdTarget(cfg *Config, target string) (host string, port int, vpcID string, err error) {
	switch {
	case strings.HasPrefix(target, "rm-"):
		return resolveRDSTarget(cfg, target)
	case strings.HasPrefix(target, "r-"):
		return resolveRedisTarget(cfg, target)
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

func resolveRDSTarget(cfg *Config, id string) (string, int, string, error) {
	client, err := NewRDSClient(cfg)
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
		// RDS ConnectionString rarely embeds a port; fall back to the engine
		// default. Users who know a non-standard port can pass host:port.
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

func resolveRedisTarget(cfg *Config, id string) (string, int, string, error) {
	client, err := NewRedisClient(cfg)
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

// pickJumpHost chooses an ECS to relay through. Priority:
//  1. --via override (resolved via existing name/pattern matcher)
//  2. Any Running instance in the target's VPC
//  3. Any Running instance at all (with a warning)
func pickJumpHost(cache *Cache, vpcID, override string) (*Instance, error) {
	if override != "" {
		inst, err := resolveInstance(cache, override)
		if err != nil {
			return nil, fmt.Errorf("--via %s: %w", override, err)
		}
		return inst, nil
	}

	instances, err := cache.Load()
	if err != nil {
		return nil, err
	}
	var fallback *Instance
	for i := range instances {
		if instances[i].Status != "Running" {
			continue
		}
		if vpcID != "" && instances[i].VpcID == vpcID {
			return &instances[i], nil
		}
		if fallback == nil {
			fallback = &instances[i]
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

// setupSocatRelay starts `socat TCP-LISTEN:... TCP:remoteHost:port` on the jump
// host and returns the listen port, its PID, and a cleanup function.
// Installs socat via the distro's package manager if missing.
func setupSocatRelay(client *AliyunClient, jumpID, remoteHost string, remotePort int) (int, string, func(), error) {
	socatPort := findFreePortInRange(19000, 19999)
	quoted := shellQuote(remoteHost)
	startCmd := fmt.Sprintf("nohup socat TCP-LISTEN:%d,fork,reuseaddr TCP:'%s':%d &>/dev/null & echo $!",
		socatPort, quoted, remotePort)

	pid, err := trySocatStart(client, jumpID, startCmd)
	if err == nil {
		return socatPort, pid, mkSocatCleanup(client, jumpID, pid), nil
	}

	// Retry after installing socat. Installer is safe to run repeatedly.
	fmt.Fprintln(os.Stderr, "⚙️  安装 socat ...")
	_, _ = client.RunCommand(jumpID, `which socat >/dev/null 2>&1 || {
  if command -v apt-get >/dev/null; then apt-get install -y socat;
  elif command -v dnf >/dev/null; then dnf install -y socat;
  elif command -v yum >/dev/null; then yum install -y socat;
  elif command -v apk >/dev/null; then apk add --no-cache socat;
  else exit 127; fi
}`, 120)
	pid, err = trySocatStart(client, jumpID, startCmd)
	if err != nil {
		return 0, "", nil, fmt.Errorf("socat 仍无法启动: %w", err)
	}
	return socatPort, pid, mkSocatCleanup(client, jumpID, pid), nil
}

func trySocatStart(client *AliyunClient, jumpID, startCmd string) (string, error) {
	res, err := client.RunCommand(jumpID, startCmd, 10)
	if err != nil {
		return "", err
	}
	pid := strings.TrimSpace(decodeOutput(res.Output))
	if pid == "" {
		return "", fmt.Errorf("empty PID")
	}
	if _, perr := strconv.Atoi(pid); perr != nil {
		return "", fmt.Errorf("non-numeric PID %q (socat 可能未安装)", pid)
	}
	return pid, nil
}

func mkSocatCleanup(client *AliyunClient, jumpID, pid string) func() {
	return func() {
		if pid == "" {
			return
		}
		_, _ = client.RunCommand(jumpID, fmt.Sprintf("kill %s 2>/dev/null", shellQuote(pid)), 5)
	}
}

// findFreePortInRange tries to find an unused local port in [start, end].
// Used to pick socat ports on the remote — local uniqueness also matters
// because Cloud Assistant port-forward can only have one session per port.
func findFreePortInRange(start, end int) int {
	// Start at a pseudo-random offset derived from time to avoid multiple
	// concurrent tssh fwd sessions always trying the same port first.
	return start + int(findFreePort())%(end-start+1)
}

func printFwdHelp() {
	fmt.Println(`用法: tssh fwd <target> [--via <name>] [--local <port>] [-j] [-q]

零配置端口转发. 自动挑同 VPC 的 ECS 当跳板, 自动分配本地端口.

target 格式:
  host:port         任意 host + port         (例: rds.internal:3306)
  ip:port           任意内网 IP              (例: 10.0.0.5:8080)
  rm-xxxxxxxx       RDS 实例 ID (自动查连接串和 VPC)
  r-xxxxxxxx        Redis 实例 ID (自动查地址和 VPC)

选项:
  --via <name>      指定跳板机, 不写就自动挑同 VPC 的第一台 Running ECS
  -p, --local <p>   固定本地端口, 不写就自动分配一个空闲端口
  -j, --json        启动成功后 stdout 打印一行 JSON, AI/脚本可 parse
  -q, --quiet       静默模式 (只输出错误)

JSON 模式输出 (stdout 一行):
  {"local_port":54321,"host":"rds.internal","remote_port":3306,"jump":"prod-jump","jump_id":"i-...","pid":12345}

示例:
  tssh fwd rds-prod.internal:3306
  tssh fwd 10.0.0.5:8080
  tssh fwd rm-2zxxxxxxxx
  tssh fwd r-bpxxxxxxxx --local 6380
  tssh fwd 10.0.0.5:8080 --via prod-jump

想同时转发多个端口, 或要把 env 注入 Spring 进程:
  tssh run --to mysql=rm-xxx,redis=r-xxx -- ./gradlew bootRun
想所有 TCP 流量都走远端 VPC:
  tssh socks prod-jump     (SOCKS5, JVM/HTTP 友好)
  tssh vpn prod-jump       (TUN 透明代理, Kafka/MQ 也吃)`)
}
