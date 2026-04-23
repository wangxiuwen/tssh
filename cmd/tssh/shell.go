package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

// cmdShell drops the user into a subshell whose env already points all TCP
// traffic at the remote ECS via SOCKS5. JDBC/HTTP/curl/git/go will transparently
// talk through the proxy; Kafka / Lettuce / RocketMQ clients that ignore
// ALL_PROXY need tssh vpn instead.
//
//	tssh shell prod-jump
//	(prod-jump) $ ./gradlew bootRun
//	(prod-jump) $ curl internal-svc.example   # goes through prod-jump
//	(prod-jump) $ exit                        # everything cleaned up
func cmdShell(args []string) {
	localPort := 1080
	remotePort := 19080
	shellOverride := ""
	var target string
	var jsonMode bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-j", "--json":
			jsonMode = true
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
		case "--shell":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "❌ --shell 需要一个路径")
				os.Exit(2)
			}
			shellOverride = args[i+1]
			i++
		case "-h", "--help":
			printShellHelp()
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
		printShellHelp()
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

	cleanup := func() {
		if pid != "" {
			_, _ = client.RunCommand(inst.ID, fmt.Sprintf("kill %s 2>/dev/null", shellQuote(pid)), 5)
		}
	}
	defer cleanup()

	stop, err := startPortForwardBgWithCancel(cfg, inst.ID, localPort, remotePort)
	if err != nil {
		cleanup()
		fatal(err, "portforward")
	}
	defer stop()

	// Resolve which shell to spawn. User override > $SHELL > /bin/bash.
	shellPath := shellOverride
	if shellPath == "" {
		shellPath = os.Getenv("SHELL")
	}
	if shellPath == "" {
		shellPath = "/bin/bash"
	}
	shellName := shellPath
	if idx := strings.LastIndex(shellName, "/"); idx >= 0 {
		shellName = shellName[idx+1:]
	}

	if jsonMode {
		payload := map[string]interface{}{
			"local_port": localPort,
			"proxy":      fmt.Sprintf("socks5h://127.0.0.1:%d", localPort),
			"via":        inst.Name,
			"jump_id":    inst.ID,
			"shell":      shellPath,
		}
		b, _ := json.Marshal(payload)
		fmt.Println(string(b))
		os.Stdout.Sync()
	} else {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "🧦 子 shell 准备就绪 — 所有 TCP 通过 %s\n", inst.Name)
		fmt.Fprintf(os.Stderr, "   ALL_PROXY=socks5h://127.0.0.1:%d\n", localPort)
		fmt.Fprintf(os.Stderr, "   JVM 自动走 SOCKS (JAVA_TOOL_OPTIONS)\n")
		fmt.Fprintln(os.Stderr, "   退出子 shell (exit / Ctrl-D) 会自动关闭 SOCKS.")
		fmt.Fprintln(os.Stderr)
	}

	env := buildShellEnv(os.Environ(), localPort, inst.Name, shellName)

	c := exec.Command(shellPath)
	c.Env = env
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	// Forward signals to the shell so Ctrl-C inside a running command works.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			if c.Process != nil {
				_ = c.Process.Signal(sig)
			}
		}
	}()

	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "❌ shell: %v\n", err)
		os.Exit(1)
	}
}

// buildShellEnv adds the proxy-related env vars to a copy of parent. Covers:
//   - curl / wget / git / go  →   ALL_PROXY / http_proxy / https_proxy (lower AND upper case)
//   - JVM                      →  JAVA_TOOL_OPTIONS with socksProxyHost/Port
//   - prompt hint             →   TSSH_SHELL_HOST + PS1-like nudge for interactive users
//
// Upper AND lowercase forms are both set because historical tools check one or
// the other; setting both removes a whole class of "why doesn't X go through
// the proxy" debugging sessions.
func buildShellEnv(parent []string, localPort int, host, shellName string) []string {
	proxyURL := fmt.Sprintf("socks5h://127.0.0.1:%d", localPort)
	jvm := fmt.Sprintf("-DsocksProxyHost=127.0.0.1 -DsocksProxyPort=%d", localPort)

	// Strip anything we're about to overwrite so the values come out clean.
	drop := map[string]bool{
		"ALL_PROXY":       true,
		"all_proxy":       true,
		"HTTP_PROXY":      true,
		"http_proxy":      true,
		"HTTPS_PROXY":     true,
		"https_proxy":     true,
		"NO_PROXY":        true,
		"no_proxy":        true,
		"TSSH_SHELL_HOST": true,
	}
	// JAVA_TOOL_OPTIONS: append rather than replace — CI systems sometimes ship
	// harmless flags in it (e.g. -XshowSettings:vm).
	var keptJVM string
	kept := parent[:0]
	for _, kv := range parent {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			kept = append(kept, kv)
			continue
		}
		key := kv[:eq]
		if key == "JAVA_TOOL_OPTIONS" {
			keptJVM = kv[eq+1:]
			continue
		}
		if drop[key] {
			continue
		}
		kept = append(kept, kv)
	}

	extended := append([]string{}, kept...)
	extended = append(extended,
		"ALL_PROXY="+proxyURL,
		"all_proxy="+proxyURL,
		"HTTP_PROXY="+proxyURL,
		"http_proxy="+proxyURL,
		"HTTPS_PROXY="+proxyURL,
		"https_proxy="+proxyURL,
		// Keep common localhost endpoints off the proxy so local tools still work.
		"NO_PROXY=localhost,127.0.0.1,::1",
		"no_proxy=localhost,127.0.0.1,::1",
		"TSSH_SHELL_HOST="+host,
	)

	mergedJVM := jvm
	if keptJVM != "" {
		mergedJVM = keptJVM + " " + jvm
	}
	extended = append(extended, "JAVA_TOOL_OPTIONS="+mergedJVM)

	// Prompt nudge for bash/zsh: users can opt into this in their rc; we only
	// set a hint variable and it is their decision whether to add
	//   PS1="(tssh:$TSSH_SHELL_HOST) $PS1"
	// to ~/.bashrc. Forcing PS1 here doesn't work reliably across bash/zsh.
	_ = shellName
	return extended
}

func printShellHelp() {
	fmt.Println(`用法: tssh shell <name> [-p <local-port>] [--shell <path>]

起一个子 shell, 把所有 TCP 出站通过远端 ECS 的 SOCKS5 代理.
体感 = "登进了这台 ECS" 但实际 shell 还是本地的.

选项:
  -p, --port <port>     本地 SOCKS5 监听端口 (默认 1080)
  --shell <path>        指定 shell (默认 $SHELL, 否则 /bin/bash)
  -j, --json            spawn shell 前 stdout 打印一行 JSON (AI 上下文)

JSON 输出:
  {"local_port":1080,"proxy":"socks5h://127.0.0.1:1080","via":"prod-jump","jump_id":"i-...","shell":"/bin/zsh"}

子 shell 内自动有的环境变量:
  ALL_PROXY / all_proxy       socks5h://127.0.0.1:<port>
  HTTP_PROXY / HTTPS_PROXY    同上 (覆盖 curl/wget/git/go 等)
  NO_PROXY                    localhost,127.0.0.1,::1
  JAVA_TOOL_OPTIONS           追加 -DsocksProxyHost/Port (JVM 自动走)
  TSSH_SHELL_HOST             = 跳板名 (用于 PS1 提示)

示例:
  tssh shell prod-jump
  (在子 shell 里)  ./gradlew bootRun   # Spring 自动连远端 RDS/Redis/HTTP
  (在子 shell 里)  curl rds.internal   # 走 SOCKS
  (在子 shell 里)  exit                # 清理并回到原 shell

可选: 在 ~/.bashrc 里加这行可以让子 shell 提示标识出来
  [ -n "$TSSH_SHELL_HOST" ] && PS1="(tssh:$TSSH_SHELL_HOST) $PS1"

注意:
  - Spring Boot + JDBC/HTTP 客户端: OK (JVM 原生支持 SOCKS)
  - Kafka / RocketMQ / Lettuce 等自写 socket 的库通常不吃 SOCKS
  - 这类场景用 tssh vpn <name> 走 TUN 透明代理`)
}
