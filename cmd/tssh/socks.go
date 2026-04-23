package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

// cmdSocks starts microsocks on the target instance and forwards a local
// TCP port to it over Cloud Assistant. The SOCKS5 server binds 127.0.0.1 on
// the remote (not the instance's private IP) so only the tunneled connection
// can reach it — no VPC or security-group exposure risk.
//
// Usage:
//   tssh socks <name>                 local 1080 → remote microsocks
//   tssh socks <name> -p 1081         custom local port
//   tssh socks <name> --remote 19080  override remote listen port
func cmdSocks(args []string) {
	localPort := 1080
	remotePort := 19080
	var target string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-p", "--port":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "❌ -p 需要一个端口号")
				os.Exit(2)
			}
			v, err := strconv.Atoi(args[i+1])
			if err != nil || v <= 0 || v > 65535 {
				fmt.Fprintf(os.Stderr, "❌ 无效本地端口: %s\n", args[i+1])
				os.Exit(2)
			}
			localPort = v
			i++
		case "--remote":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "❌ --remote 需要一个端口号")
				os.Exit(2)
			}
			v, err := strconv.Atoi(args[i+1])
			if err != nil || v <= 0 || v > 65535 {
				fmt.Fprintf(os.Stderr, "❌ 无效远端端口: %s\n", args[i+1])
				os.Exit(2)
			}
			remotePort = v
			i++
		case "-h", "--help":
			printSocksHelp()
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
		printSocksHelp()
		os.Exit(1)
	}

	cache := getCache()
	ensureCache(cache)
	inst := resolveInstanceOrExit(cache, target)

	cfg := mustLoadConfig()
	client, err := NewAliyunClient(cfg)
	fatal(err, "create client")

	fmt.Fprintf(os.Stderr, "🔌 在 %s 上启动 SOCKS5 (microsocks)...\n", inst.Name)
	pid, err := startRemoteSocks(client, inst.ID, remotePort)
	fatal(err, "start microsocks")

	// Always try to kill the remote microsocks on exit, no matter how we leave.
	cleanup := func() {
		if pid == "" {
			return
		}
		_, _ = client.RunCommand(inst.ID, fmt.Sprintf("kill %s 2>/dev/null", shellQuote(pid)), 5)
	}
	defer cleanup()

	stop, err := startPortForwardBgWithCancel(cfg, inst.ID, localPort, remotePort)
	if err != nil {
		cleanup()
		fatal(err, "portforward")
	}
	defer stop()

	fmt.Println()
	fmt.Printf("🧦 SOCKS5 proxy ready: 127.0.0.1:%d  (via %s)\n", localPort, inst.Name)
	fmt.Println()
	fmt.Println("示例用法:")
	fmt.Printf("  curl --socks5-hostname 127.0.0.1:%d https://internal.example.com\n", localPort)
	fmt.Printf("  export ALL_PROXY=socks5h://127.0.0.1:%d\n", localPort)
	fmt.Printf("  export JAVA_TOOL_OPTIONS='-DsocksProxyHost=127.0.0.1 -DsocksProxyPort=%d'\n", localPort)
	fmt.Println()
	fmt.Println("按 Ctrl+C 退出 (会自动关闭远端 microsocks)")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	fmt.Fprintln(os.Stderr, "\n🛑 清理中...")
}

// startRemoteSocks makes sure microsocks is installed + running on 127.0.0.1.
// Returns its PID so the caller can kill it on shutdown. Binding to loopback
// (not the instance's private IP) means no SOCKS5 exposure to the VPC even if
// the security group is lax.
func startRemoteSocks(client *AliyunClient, instanceID string, port int) (string, error) {
	startCmd := fmt.Sprintf("nohup microsocks -i 127.0.0.1 -p %d >/tmp/tssh-socks.log 2>&1 & echo $!", port)

	pid, err := tryStartSocks(client, instanceID, startCmd)
	if err == nil {
		return pid, nil
	}

	// First attempt failed (likely microsocks not installed). Try to install.
	fmt.Fprintln(os.Stderr, "⚙️  安装 microsocks ...")
	installCmd := `which microsocks >/dev/null 2>&1 || {
  if command -v apt-get >/dev/null; then apt-get install -y microsocks;
  elif command -v dnf >/dev/null; then dnf install -y epel-release microsocks 2>/dev/null || dnf install -y microsocks;
  elif command -v yum >/dev/null; then yum install -y epel-release microsocks 2>/dev/null || yum install -y microsocks;
  elif command -v apk >/dev/null; then apk add --no-cache microsocks;
  else echo "no supported package manager" >&2; exit 127; fi
}`
	if _, err := client.RunCommand(instanceID, installCmd, 120); err != nil {
		return "", fmt.Errorf("install microsocks: %w (原错误: %v)", err, err)
	}

	pid, err = tryStartSocks(client, instanceID, startCmd)
	if err != nil {
		return "", fmt.Errorf(`microsocks 仍无法启动, 请手动安装:
    apt install microsocks      # Debian/Ubuntu
    yum install epel-release && yum install microsocks   # CentOS/RHEL
原错误: %w`, err)
	}
	return pid, nil
}

// tryStartSocks runs the start script and returns the PID string.
// An empty PID or exit!=0 is treated as "not installed" so the caller knows
// to attempt installation.
func tryStartSocks(client *AliyunClient, instanceID, startCmd string) (string, error) {
	res, err := client.RunCommand(instanceID, startCmd, 10)
	if err != nil {
		return "", err
	}
	pid := strings.TrimSpace(decodeOutput(res.Output))
	// PID must be a positive integer; any non-numeric output means the shell
	// wrote an error (e.g. "microsocks: command not found") before the echo.
	if pid == "" {
		return "", fmt.Errorf("empty PID")
	}
	if _, perr := strconv.Atoi(pid); perr != nil {
		return "", fmt.Errorf("invalid PID %q (microsocks 可能未安装)", pid)
	}
	return pid, nil
}

func printSocksHelp() {
	fmt.Println(`用法: tssh socks <name> [-p <local-port>] [--remote <remote-port>]

在远端 ECS 启动 microsocks 并打开本地 SOCKS5 代理. 适用场景:
  - 本地 Spring/Python 等程序通过 SOCKS 访问远端 VPC 的 MySQL/HTTP/DNS
  - curl --socks5-hostname 访问内网资源
  - JVM: -DsocksProxyHost / 环境变量 ALL_PROXY

选项:
  -p, --port <port>     本地监听端口 (默认 1080)
  --remote <port>       远端 microsocks 端口 (默认 19080, 绑 127.0.0.1)

说明:
  - 远端 microsocks 只绑 127.0.0.1, 流量经 Cloud Assistant 传入, 无 VPC 暴露
  - 首次使用会自动 apt/yum/dnf/apk 安装 microsocks
  - Ctrl+C 退出时自动 kill 远端 microsocks`)
}
