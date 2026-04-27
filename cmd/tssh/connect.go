package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// requireRunning 拦截 Stopped/Starting/Stopping 等状态; Cloud Assistant 反向通道
// 在 not-running 实例上要么超时 hang, 要么返回看不懂的 InvalidInstance.NotRunning,
// 不如直接挡掉给用户清晰提示.
func requireRunning(inst *Instance, action string) {
	if inst.Status == "Running" {
		return
	}
	fmt.Fprintf(os.Stderr, "⛔ %s (%s) 当前状态: %s, 不能 %s\n", inst.Name, inst.ID, inst.Status, action)
	fmt.Fprintf(os.Stderr, "   先开机: tssh start %s\n", inst.Name)
	os.Exit(1)
}

// cmdConnect connects interactively
func cmdConnect(target string) {
	cache := getCache()
	ensureCache(cache)

	inst := resolveInstanceOrExit(cache, target)
	requireRunning(inst, "连接")

	config := mustLoadConfig()

	fmt.Printf("🔗 连接: %s (%s / %s)\n", inst.Name, inst.ID, inst.PrivateIP)
	err := ConnectSession(config, inst.ID)
	fatal(err, "connect")
}

// cmdRemoteExec runs a command on a single instance (SSH-like)
func cmdRemoteExec(target, command string, timeout int) {
	cache := getCache()
	inst := resolveInstanceOrExit(cache, target)
	requireRunning(inst, "执行命令")

	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	if timeout <= 0 {
		timeout = 60
	}

	result, err := client.RunCommand(inst.ID, command, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %s: %v\n", inst.Name, err)
		if result != nil {
			os.Exit(result.ExitCode)
		}
		os.Exit(255) // SSH convention: 255 = SSH/tssh internal error
	}

	fmt.Print(decodeOutput(result.Output))
	os.Exit(result.ExitCode)
}

// cmdPortForward handles -L port forwarding with REMOTE HOST support
// Supports syntax sugar:
//
//	-L 3306           → 3306:localhost:3306
//	-L 3306:dbhost    → 3306:dbhost:3306
//	-L 3306:dbhost:5432 → full form
func cmdPortForward(target, spec string) {
	parts := strings.SplitN(spec, ":", 3)
	switch len(parts) {
	case 1:
		// -L 3306 → 3306:localhost:3306
		parts = []string{parts[0], "localhost", parts[0]}
	case 2:
		// -L 3306:dbhost → 3306:dbhost:3306
		parts = []string{parts[0], parts[1], parts[0]}
	case 3:
		// full form, keep as is
	default:
		fmt.Fprintln(os.Stderr, "❌ 格式: -L <port> 或 -L <local>:<host>:<remote>")
		os.Exit(1)
	}

	localPort, err := strconv.Atoi(parts[0])
	fatal(err, "invalid local port")
	if localPort <= 0 || localPort > 65535 {
		fmt.Fprintf(os.Stderr, "❌ 本地端口超出范围 (1-65535): %d\n", localPort)
		os.Exit(1)
	}
	remoteHost := parts[1]
	remotePort, err := strconv.Atoi(parts[2])
	fatal(err, "invalid remote port")
	if remotePort <= 0 || remotePort > 65535 {
		fmt.Fprintf(os.Stderr, "❌ 远程端口超出范围 (1-65535): %d\n", remotePort)
		os.Exit(1)
	}

	cache := getCache()
	inst := resolveInstanceOrExit(cache, target)
	requireRunning(inst, "端口转发")

	config := mustLoadConfig()

	fmt.Printf("🔗 %s (%s)\n", inst.Name, inst.ID)
	fmt.Printf("📡 端口转发: 127.0.0.1:%d → %s:%d\n", localPort, remoteHost, remotePort)

	if remoteHost == "localhost" || remoteHost == "127.0.0.1" {
		fatal(startNativePortForward(config, inst.ID, localPort, remotePort), "portforward")
	} else {
		fmt.Fprintf(os.Stderr, "📡 通过 %s 中转到 %s:%d\n", inst.Name, remoteHost, remotePort)

		client, err := NewAliyunClient(config)
		fatal(err, "create client")

		socatPort := 19999
		// remoteHost comes from user-supplied -L spec; quote to prevent shell
		// injection via e.g. -L 3306:$(evil):3306.
		socatCmd := fmt.Sprintf("nohup socat TCP-LISTEN:%d,fork,reuseaddr TCP:'%s':%d &>/dev/null & echo $!", socatPort, shellQuote(remoteHost), remotePort)
		result, err := client.RunCommand(inst.ID, socatCmd, 10)
		if err != nil {
			fmt.Fprintln(os.Stderr, "⚙️  安装 socat...")
			client.RunCommand(inst.ID, "which socat || (apt-get install -y socat 2>/dev/null || yum install -y socat 2>/dev/null)", 30)
			result, err = client.RunCommand(inst.ID, socatCmd, 10)
			fatal(err, "start socat")
		}
		socatPid := strings.TrimSpace(decodeOutput(result.Output))
		// echo $! should be numeric; if remote emitted anything else (error/banner),
		// kill with that as arg could shell-inject. Skip cleanup on malformed output.
		if _, convErr := strconv.Atoi(socatPid); convErr != nil {
			socatPid = ""
		}

		defer func() {
			if socatPid != "" {
				client.RunCommand(inst.ID, fmt.Sprintf("kill %s 2>/dev/null", socatPid), 5)
			}
		}()

		fatal(startNativePortForward(config, inst.ID, localPort, socatPort), "portforward")
	}
}
