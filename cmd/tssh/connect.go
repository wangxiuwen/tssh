package main

import (
"fmt"
"os"
"strconv"
"strings"
)


// cmdConnect connects interactively
func cmdConnect(target string) {
	cache := getCache()
	ensureCache(cache)

	inst := resolveInstanceOrExit(cache, target)

	config := mustLoadConfig()

	fmt.Printf("🔗 连接: %s (%s / %s)\n", inst.Name, inst.ID, inst.PrivateIP)
	err := ConnectSession(config, inst.ID)
	fatal(err, "connect")
}

// cmdRemoteExec runs a command on a single instance (SSH-like)
func cmdRemoteExec(target, command string) {
	cache := getCache()
	inst := resolveInstanceOrExit(cache, target)

	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	result, err := client.RunCommand(inst.ID, command, 60)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %s: %v\n", inst.Name, err)
		if result != nil {
			os.Exit(result.ExitCode)
		}
		os.Exit(1)
	}

	fmt.Print(decodeOutput(result.Output))
	os.Exit(result.ExitCode)
}

// cmdPortForward handles -L port forwarding with REMOTE HOST support
// Supports syntax sugar:
//   -L 3306           → 3306:localhost:3306
//   -L 3306:dbhost    → 3306:dbhost:3306
//   -L 3306:dbhost:5432 → full form
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
	remoteHost := parts[1]
	remotePort, err := strconv.Atoi(parts[2])
	fatal(err, "invalid remote port")

	cache := getCache()
	inst := resolveInstanceOrExit(cache, target)

	config := mustLoadConfig()

	fmt.Printf("🔗 %s (%s)\n", inst.Name, inst.ID)
	fmt.Printf("📡 端口转发: 127.0.0.1:%d → %s:%d\n", localPort, remoteHost, remotePort)

	if remoteHost == "localhost" || remoteHost == "127.0.0.1" {
		cmd := startPortForward(config, inst.ID, localPort, remotePort)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		fatal(cmd.Run(), "portforward")
	} else {
		// Remote host forwarding: use socat on the remote machine via portforward
		// First, set up portforward to a high port on the remote machine
		// Then use socat/ssh tunnel to reach the actual remote host
		fmt.Fprintf(os.Stderr, "📡 通过 %s 中转到 %s:%d\n", inst.Name, remoteHost, remotePort)

		// Strategy: portforward to an ephemeral port on the ECS instance,
		// then run socat on the ECS to relay to the actual remote host.
		// Step 1: Run socat in background on remote
		client, err := NewAliyunClient(config)
		fatal(err, "create client")

		socatPort := 19999
		socatCmd := fmt.Sprintf("nohup socat TCP-LISTEN:%d,fork,reuseaddr TCP:%s:%d &>/dev/null & echo $!", socatPort, remoteHost, remotePort)
		result, err := client.RunCommand(inst.ID, socatCmd, 10)
		if err != nil {
			// Try installing socat
			fmt.Fprintln(os.Stderr, "⚙️  安装 socat...")
			client.RunCommand(inst.ID, "which socat || (apt-get install -y socat 2>/dev/null || yum install -y socat 2>/dev/null)", 30)
			result, err = client.RunCommand(inst.ID, socatCmd, 10)
			fatal(err, "start socat")
		}
		socatPid := strings.TrimSpace(decodeOutput(result.Output))

		// Step 2: portforward to socat port
		cmd := startPortForward(config, inst.ID, localPort, socatPort)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		// Cleanup socat on exit
		defer func() {
			if socatPid != "" {
				client.RunCommand(inst.ID, fmt.Sprintf("kill %s 2>/dev/null", socatPid), 5)
			}
		}()

		fatal(cmd.Run(), "portforward")
	}
}
