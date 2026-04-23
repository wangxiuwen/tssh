package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// cmdVPN gives the user a true transparent-proxy feel: any TCP/UDP packet
// whose destination falls in the supplied CIDR list is routed through the
// chosen ECS. Kafka / RocketMQ / gRPC streaming / Lettuce — libraries that
// ignore SOCKS — all "just work" because this operates at layer 3.
//
// Required stack (we orchestrate, don't reimplement):
//   - Remote microsocks (already auto-installed by tssh socks)
//   - Local port-forward to the microsocks port
//   - `tun2socks` binary on PATH (e.g. `go install
//     github.com/xjasonlyu/tun2socks/v2@latest`) — bridges TUN <-> SOCKS5
//   - sudo for TUN creation + route table edits
//
// Deliberately NOT vendored in-process: tun2socks is GPL-3 and pulling its
// netstack in-process would force GPL on tssh. Shelling out keeps license
// boundaries clean and lets the user upgrade tun2socks independently.
//
//	sudo tssh vpn prod-jump --cidr 10.0.0.0/16
//	sudo tssh vpn prod-jump --cidr 10.0.0.0/16,172.16.0.0/12
func cmdVPN(args []string) {
	cidrList := ""
	tunName := ""
	var target string
	var jsonMode bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-j", "--json":
			jsonMode = true
		case "--cidr":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "❌ --cidr 需要一个值 (如 10.0.0.0/16,172.16.0.0/12)")
				os.Exit(2)
			}
			cidrList = args[i+1]
			i++
		case "--tun":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "❌ --tun 需要一个设备名")
				os.Exit(2)
			}
			tunName = args[i+1]
			i++
		case "-h", "--help":
			printVPNHelp()
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

	if target == "" || cidrList == "" {
		printVPNHelp()
		os.Exit(1)
	}

	// Preflight checks — fail fast, with actionable messages. The alternative
	// (fail mid-setup) leaves dangling microsocks/tun2socks/routes.
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		fatalMsg("tssh vpn 目前只支持 Linux / macOS (当前: " + runtime.GOOS + ")")
	}
	if os.Geteuid() != 0 {
		fatalMsg("tssh vpn 需要 root (添加路由 + 创建 TUN 设备)\n   请用: sudo tssh vpn ...")
	}
	tun2socksBin, err := exec.LookPath("tun2socks")
	if err != nil {
		fatalMsg(`未找到 tun2socks 二进制. 安装方式:

    # 任意装过 Go 的机器
    go install github.com/xjasonlyu/tun2socks/v2@latest

    # 或下载官方 release
    # https://github.com/xjasonlyu/tun2socks/releases

确保 $PATH 或 /usr/local/bin 能找到 tun2socks 后重试.
sudo 下 PATH 可能被重置, 可用: sudo env "PATH=$PATH" tssh vpn ...`)
	}

	cidrs := parseCIDRList(cidrList)
	if len(cidrs) == 0 {
		fatalMsg("--cidr 解析失败或为空")
	}

	if tunName == "" {
		tunName = defaultTunName()
	}

	cache := getCache()
	ensureCache(cache)
	inst := resolveInstanceOrExit(cache, target)

	cfg := mustLoadConfig()
	client, err := NewAliyunClient(cfg)
	fatal(err, "create client")

	// --- 1. Remote microsocks ---
	fmt.Fprintf(os.Stderr, "🔌 在 %s 上启动 SOCKS5 (microsocks)...\n", inst.Name)
	socksRemotePort := 19080
	socksPID, err := startRemoteSocks(client, inst.ID, socksRemotePort)
	fatal(err, "start microsocks")
	cleanupSocks := func() {
		_, _ = client.RunCommand(inst.ID, fmt.Sprintf("kill %s 2>/dev/null", shellQuote(socksPID)), 5)
	}

	// --- 2. Local port-forward to microsocks ---
	socksLocalPort := findFreePort()
	stopPF, err := startPortForwardBgWithCancel(cfg, inst.ID, socksLocalPort, socksRemotePort)
	if err != nil {
		cleanupSocks()
		fatal(err, "portforward")
	}

	// --- 3. Spawn tun2socks ---
	fmt.Fprintf(os.Stderr, "🔧 启动 tun2socks → %s\n", tunName)
	t2sCmd := exec.Command(tun2socksBin,
		"-device", tunName,
		"-proxy", fmt.Sprintf("socks5://127.0.0.1:%d", socksLocalPort),
		"-loglevel", "warning",
	)
	t2sCmd.Stdout = os.Stderr
	t2sCmd.Stderr = os.Stderr
	if err := t2sCmd.Start(); err != nil {
		stopPF()
		cleanupSocks()
		fatal(err, "start tun2socks")
	}
	cleanupTun2socks := func() {
		if t2sCmd.Process != nil {
			_ = t2sCmd.Process.Signal(syscall.SIGTERM)
			// Give it ~2s to exit; kill hard otherwise so we always return.
			done := make(chan struct{})
			go func() { _ = t2sCmd.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = t2sCmd.Process.Kill()
			}
		}
	}

	// tun2socks creates the device asynchronously. On macOS it allocates the
	// next free utun; waitForTun() polls until ifconfig shows it up.
	if err := waitForTun(tunName, 5*time.Second); err != nil {
		cleanupTun2socks()
		stopPF()
		cleanupSocks()
		fatal(err, "wait for tun device")
	}

	// --- 4. Routes ---
	var addedRoutes [][]string
	undoRoutes := func() {
		for _, args := range addedRoutes {
			_ = exec.Command(args[0], args[1:]...).Run()
		}
	}
	for _, cidr := range cidrs {
		cmdArgs, undoArgs := routeAddCmd(tunName, cidr)
		out, err := exec.Command(cmdArgs[0], cmdArgs[1:]...).CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ 添加路由失败 %s: %v\n   %s\n", cidr, err, strings.TrimSpace(string(out)))
			undoRoutes()
			cleanupTun2socks()
			stopPF()
			cleanupSocks()
			os.Exit(1)
		}
		addedRoutes = append(addedRoutes, undoArgs)
	}

	if jsonMode {
		payload := map[string]interface{}{
			"tun":              tunName,
			"cidrs":            cidrs,
			"socks_local_port": socksLocalPort,
			"via":              inst.Name,
			"jump_id":          inst.ID,
			"tun2socks_pid":    t2sCmd.Process.Pid,
			"pid":              os.Getpid(),
		}
		b, _ := json.Marshal(payload)
		fmt.Println(string(b))
		os.Stdout.Sync()
	} else {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "🌐 VPN 上线 — CIDR %s 的所有 TCP/UDP 通过 %s\n", cidrList, inst.Name)
		fmt.Fprintf(os.Stderr, "   TUN: %s   SOCKS5: 127.0.0.1:%d   tun2socks PID: %d\n", tunName, socksLocalPort, t2sCmd.Process.Pid)
		fmt.Fprintln(os.Stderr, "   按 Ctrl+C 退出 — 路由/tun 设备/远端 microsocks 都会自动清理.")
	}

	// --- 5. Wait for Ctrl-C, then unwind in reverse order.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	fmt.Fprintln(os.Stderr, "\n🛑 清理中...")

	undoRoutes()
	cleanupTun2socks()
	stopPF()
	cleanupSocks()
	fmt.Fprintln(os.Stderr, "✅ 已清理")
}

// parseCIDRList splits "10.0.0.0/16,172.16.0.0/12" into a slice, trimming
// whitespace. Doesn't validate — the route command will reject garbage more
// informatively than we would.
func parseCIDRList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// defaultTunName picks a TUN device name appropriate for the platform.
// macOS uses utunN (kernel assigns the next free N when passed "utun"),
// Linux accepts any name; we pick tssh0 so the device is greppable.
func defaultTunName() string {
	if runtime.GOOS == "darwin" {
		// tun2socks accepts "utun" on darwin and asks the kernel for a free slot.
		return "utun"
	}
	return "tssh0"
}

// waitForTun polls `ifconfig <name>` until the interface appears or deadline
// hits. On macOS the utun number is only known once tun2socks has started.
// For the "utun" auto-allocation case we can't pre-know the number, so we
// skip the check and trust tun2socks.
func waitForTun(name string, timeout time.Duration) error {
	if name == "utun" {
		// Kernel-assigned; no deterministic name to poll. Small sleep gives
		// tun2socks time to initialize before we start adding routes.
		time.Sleep(500 * time.Millisecond)
		return nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := exec.Command("ifconfig", name).Run(); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("TUN 设备 %s 未在 %s 内就绪", name, timeout)
}

// routeAddCmd returns (add, del) argv pairs for the current platform.
// Linux: `ip route add <cidr> dev <tun>`
// macOS: `route -n add -net <cidr> -interface <tun>`
// We keep the undo command alongside the do so cleanup can't drift out of
// sync with changes here.
func routeAddCmd(tun, cidr string) (add, del []string) {
	if runtime.GOOS == "darwin" {
		// -net needs host/prefix; "route" on darwin accepts CIDR notation.
		add = []string{"route", "-n", "add", "-net", cidr, "-interface", tun}
		del = []string{"route", "-n", "delete", "-net", cidr, "-interface", tun}
		return
	}
	// Linux
	add = []string{"ip", "route", "add", cidr, "dev", tun}
	del = []string{"ip", "route", "del", cidr, "dev", tun}
	return
}

// fatalMsg — declared in main.go as a wrapper over shared.FatalMsg now.

func printVPNHelp() {
	fmt.Println(`用法: sudo tssh vpn <name> --cidr <a/b[,c/d,...]> [--tun <dev>]

L3 透明代理: 把指定 CIDR 的所有 TCP/UDP 通过远端 ECS.
体感上相当于 "登进了那台机器" — JDBC/HTTP/gRPC/Kafka/MQ/MySQL CLI 全部走.

参数:
  <name>           跳板 ECS (自动起 microsocks)
  --cidr <list>    要劫持的 CIDR, 逗号分隔 (如 10.0.0.0/16 或多段)
  --tun <dev>      TUN 名 (默认: Linux tssh0, macOS utun 自动)
  -j, --json       VPN 上线后 stdout 打印一行 JSON (AI/脚本用)

JSON 输出:
  {"tun":"tssh0","cidrs":["10.0.0.0/16"],"socks_local_port":54321,
   "via":"prod-jump","jump_id":"i-...","tun2socks_pid":1234,"pid":5678}

依赖:
  - root / sudo (添加路由 + 创建 TUN 设备)
  - tun2socks 二进制在 PATH:
      go install github.com/xjasonlyu/tun2socks/v2@latest

示例:
  sudo tssh vpn prod-jump --cidr 10.0.0.0/16
  sudo tssh vpn prod-jump --cidr 10.0.0.0/16,172.16.0.0/12
  sudo env "PATH=$PATH" tssh vpn prod-jump --cidr 10.0.0.0/16   # sudo 下 PATH 被重置时

与其他模式对比:
  tssh fwd <target>     一个端口, 零配置, 不用 sudo
  tssh shell <host>     SOCKS5 子 shell, 不用 sudo, Kafka/MQ 不吃
  tssh socks <host>     只开 SOCKS5, 自己选客户端配代理
  tssh vpn  <host>      TUN 透明, 所有协议通吃, 要 sudo

清理:
  Ctrl+C 会按序卸载: 路由 → tun2socks → port-forward → 远端 microsocks.
  进程被 SIGKILL 强杀时路由可能残留, 手动清:
      Linux:  sudo ip route del <cidr> dev <tun>
      macOS:  sudo route -n delete -net <cidr> -interface <tun>`)
}
