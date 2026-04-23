// Package net holds the tssh "network access" subcommands — socks / fwd /
// run / shell / vpn / browser. Each one exists so a developer's laptop can
// reach private VPC resources through a jump ECS without the kubectl
// port-forward / SSH key dance.
//
// All commands take a core.Runtime and use internal/session + internal/shared.
package net

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/wangxiuwen/tssh/internal/aliyun"
	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/session"
	"github.com/wangxiuwen/tssh/internal/shared"
)

// Socks starts microsocks on the target instance and forwards a local TCP
// port to it over Cloud Assistant. The SOCKS5 server binds 127.0.0.1 on
// the remote (not the instance's private IP) so only the tunneled
// connection can reach it — no VPC or security-group exposure risk.
func Socks(rt core.Runtime, args []string) {
	localPort := 1080
	remotePort := 19080
	var target string
	var jsonMode, quietMode bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-j", "--json":
			jsonMode = true
		case "-q", "--quiet":
			quietMode = true
		case "-p", "--port":
			if i+1 >= len(args) {
				shared.FatalMsg("-p 需要一个端口号")
			}
			v, err := strconv.Atoi(args[i+1])
			if err != nil || v <= 0 || v > 65535 {
				shared.FatalMsg(fmt.Sprintf("无效本地端口: %s", args[i+1]))
			}
			localPort = v
			i++
		case "--remote":
			if i+1 >= len(args) {
				shared.FatalMsg("--remote 需要一个端口号")
			}
			v, err := strconv.Atoi(args[i+1])
			if err != nil || v <= 0 || v > 65535 {
				shared.FatalMsg(fmt.Sprintf("无效远端端口: %s", args[i+1]))
			}
			remotePort = v
			i++
		case "-h", "--help":
			printSocksHelp()
			return
		default:
			if strings.HasPrefix(args[i], "-") {
				shared.FatalMsg(fmt.Sprintf("未知选项: %s", args[i]))
			}
			if target != "" {
				shared.FatalMsg("只能指定一个 target")
			}
			target = args[i]
		}
	}

	if target == "" {
		printSocksHelp()
		os.Exit(1)
	}

	inst := rt.ResolveInstance(target)
	if inst == nil {
		os.Exit(1)
	}

	cfg := rt.LoadConfig()
	client, err := aliyun.NewClient(cfg)
	shared.Fatal(err, "create client")

	fmt.Fprintf(os.Stderr, "🔌 在 %s 上启动 SOCKS5 (microsocks)...\n", inst.Name)
	pid, err := session.StartRemoteSocks(client, inst.ID, remotePort)
	shared.Fatal(err, "start microsocks")

	cleanup := func() {
		if pid == "" {
			return
		}
		_, _ = client.RunCommand(inst.ID, fmt.Sprintf("kill %s 2>/dev/null", shared.ShellQuote(pid)), 5)
	}
	defer cleanup()

	stop, err := rt.StartPortForward(inst.ID, localPort, remotePort)
	if err != nil {
		cleanup()
		shared.Fatal(err, "portforward")
	}
	defer stop()

	if jsonMode {
		payload := map[string]any{
			"local_port": localPort,
			"proxy":      fmt.Sprintf("socks5h://127.0.0.1:%d", localPort),
			"via":        inst.Name,
			"jump_id":    inst.ID,
			"remote_pid": pid,
			"pid":        os.Getpid(),
		}
		b, _ := json.Marshal(payload)
		fmt.Println(string(b))
		os.Stdout.Sync()
	} else if !quietMode {
		fmt.Println()
		fmt.Printf("🧦 SOCKS5 proxy ready: 127.0.0.1:%d  (via %s)\n", localPort, inst.Name)
		fmt.Println()
		fmt.Println("示例用法:")
		fmt.Printf("  curl --socks5-hostname 127.0.0.1:%d https://internal.example.com\n", localPort)
		fmt.Printf("  export ALL_PROXY=socks5h://127.0.0.1:%d\n", localPort)
		fmt.Printf("  export JAVA_TOOL_OPTIONS='-DsocksProxyHost=127.0.0.1 -DsocksProxyPort=%d'\n", localPort)
		fmt.Println()
		fmt.Println("按 Ctrl+C 退出 (会自动关闭远端 microsocks)")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	if !jsonMode && !quietMode {
		fmt.Fprintln(os.Stderr, "\n🛑 清理中...")
	}
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
  -j, --json            启动成功后 stdout 打印一行 JSON, AI/脚本可 parse
  -q, --quiet           静默模式

说明:
  - 远端 microsocks 只绑 127.0.0.1, 流量经 Cloud Assistant 传入, 无 VPC 暴露
  - 首次使用会自动 apt/yum/dnf/apk 安装 microsocks
  - Ctrl+C 退出时自动 kill 远端 microsocks`)
}
