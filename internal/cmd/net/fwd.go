package net

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/shared"
)

// Fwd is the zero-config single-port forwarder. Accepts host:port, RDS ID,
// Redis ID and auto-picks a same-VPC ECS as jump host.
func Fwd(rt core.Runtime, args []string) {
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
				shared.FatalMsg("--via 需要一个 name/id")
			}
			via = args[i+1]
			i++
		case "--local", "-p":
			if i+1 >= len(args) {
				shared.FatalMsg("--local 需要一个端口号")
			}
			p, err := strconv.Atoi(args[i+1])
			if err != nil || p <= 0 || p > 65535 {
				shared.FatalMsg(fmt.Sprintf("无效本地端口: %s", args[i+1]))
			}
			localPort = p
			i++
		case "-h", "--help":
			printFwdHelp()
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
		printFwdHelp()
		os.Exit(1)
	}

	host, port, vpcID, err := ResolveFwdTarget(rt, target)
	shared.Fatal(err, "resolve target")

	jumpHost, err := PickJumpHost(rt, vpcID, via)
	shared.Fatal(err, "pick jump host")

	if localPort == 0 {
		localPort = shared.FindFreePort()
	}

	var socatPort int
	var cleanup func()
	if host == "localhost" || host == "127.0.0.1" {
		socatPort = port
	} else {
		socatPort, cleanup, err = rt.StartSocatRelay(jumpHost.ID, host, port)
		shared.Fatal(err, "setup socat relay")
	}
	if cleanup != nil {
		defer cleanup()
	}

	stop, err := rt.StartPortForward(jumpHost.ID, localPort, socatPort)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		shared.Fatal(err, "portforward")
	}
	defer stop()

	if jsonMode {
		payload := map[string]any{
			"local_port":  localPort,
			"host":        host,
			"remote_port": port,
			"jump":        jumpHost.Name,
			"jump_id":     jumpHost.ID,
			"pid":         os.Getpid(),
		}
		b, _ := json.Marshal(payload)
		fmt.Println(string(b))
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

示例:
  tssh fwd rds-prod.internal:3306
  tssh fwd 10.0.0.5:8080
  tssh fwd rm-2zxxxxxxxx
  tssh fwd r-bpxxxxxxxx --local 6380
  tssh fwd 10.0.0.5:8080 --via prod-jump`)
}
