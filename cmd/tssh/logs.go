package main

import (
	"fmt"
	"os"
	"strings"
)

// cmdLogs streams aggregated logs from all pods matching a k8s service /
// selector. Uses kubectl's native `-l <selector> --prefix` flag so each
// line carries [namespace/pod/container] — identical to `stern` behaviour
// but without needing stern installed.
//
// Because kubectl logs -f needs a streaming channel (not Cloud Assistant's
// polling RunCommand), we run it inside an interactive WebSocket session;
// Ctrl-C on the local side tears the session down and kubectl stops.
//
//	tssh logs prod-jump grafana
//	tssh logs prod-jump grafana -n monitoring --tail 200
//	tssh logs prod-jump grafana --since 5m -f
//	tssh logs prod-jump -l app=grafana,tier=frontend   # 直接传 selector
func cmdLogs(args []string) {
	namespace := ""
	tail := 100
	since := ""
	follow := true // default follow; matches user expectation ("watch the logs")
	var selector string
	var jump, svc string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--namespace":
			if i+1 >= len(args) {
				fatalMsg("-n 需要 namespace")
			}
			namespace = args[i+1]
			i++
		case "--tail":
			if i+1 >= len(args) {
				fatalMsg("--tail 需要一个数字")
			}
			tail = atoiDefault(args[i+1], -1)
			if tail < 0 {
				fatalMsg("--tail 需正整数")
			}
			i++
		case "--since":
			if i+1 >= len(args) {
				fatalMsg("--since 需要 duration")
			}
			since = args[i+1]
			i++
		case "-l", "--selector":
			if i+1 >= len(args) {
				fatalMsg("-l 需要 selector (如 app=grafana)")
			}
			selector = args[i+1]
			i++
		case "-f", "--follow":
			follow = true
		case "--no-follow":
			follow = false
		case "-h", "--help":
			printLogsHelp()
			return
		default:
			if strings.HasPrefix(args[i], "-") {
				fatalMsg(fmt.Sprintf("未知选项: %s", args[i]))
			}
			if jump == "" {
				jump = args[i]
			} else if svc == "" {
				svc = args[i]
			} else {
				fatalMsg("最多一个 svc, 多选用 -l selector")
			}
		}
	}

	if jump == "" {
		printLogsHelp()
		os.Exit(1)
	}
	if svc == "" && selector == "" {
		fatalMsg("需要提供 <svc> 或 -l <selector>")
	}

	cache := getCache()
	ensureCache(cache)
	inst := resolveInstanceOrExit(cache, jump)
	cfg := mustLoadConfig()

	var kubectlArgs []string
	if namespace != "" {
		kubectlArgs = append(kubectlArgs, "-n", shellQuote(namespace))
	}
	kubectlArgs = append(kubectlArgs, "logs", "--prefix")
	if tail > 0 {
		kubectlArgs = append(kubectlArgs, fmt.Sprintf("--tail=%d", tail))
	}
	if since != "" {
		kubectlArgs = append(kubectlArgs, "--since="+shellQuote(since))
	}
	if follow {
		kubectlArgs = append(kubectlArgs, "-f", "--max-log-requests=20")
	}
	// kubectl accepts either a single resource or -l selector; we resolve
	// svc→selector by asking kubectl to expand it implicitly:
	//   kubectl logs svc/<name>  — picks one pod, not multiple. No good.
	// So when svc is given, query the svc's selector first, then use -l.
	// But that's a second round-trip. Simpler: synthesize a heuristic
	// selector "app=<svc>" when user only gave svc name. If that doesn't
	// match, fall back to resolving the svc's selector via kubectl get svc.
	if selector == "" {
		// Resolve the svc's selector on the remote, then use it for logs.
		// One shell pipeline so we only take one API call.
		resolveAndLogs := fmt.Sprintf(
			`SEL=$(kubectl%s get svc %s -o jsonpath='{.spec.selector}' | tr -d '{}"' | tr ',' ',' | sed 's/:/=/g')
if [ -z "$SEL" ]; then echo "❌ svc %s 没找到或没 selector" >&2; exit 1; fi
exec kubectl%s %s -l "$SEL"`,
			nsFlagFor(namespace), shellQuote(svc), shellQuote(svc),
			nsFlagFor(namespace), strings.Join(kubectlArgs, " "))
		fmt.Fprintf(os.Stderr, "📡 %s — 流式聚合 svc/%s 的所有 pod 日志\n", inst.Name, svc)
		if follow {
			fmt.Fprintln(os.Stderr, "   按 Ctrl+C 退出")
		}
		fatal(ConnectSessionWithCommand(cfg, inst.ID, resolveAndLogs), "session")
		return
	}
	// Explicit selector path — simpler.
	cmd := "kubectl " + strings.Join(kubectlArgs, " ") + " -l " + shellQuote(selector)
	fmt.Fprintf(os.Stderr, "📡 %s — %s\n", inst.Name, cmd)
	if follow {
		fmt.Fprintln(os.Stderr, "   按 Ctrl+C 退出")
	}
	fatal(ConnectSessionWithCommand(cfg, inst.ID, cmd), "session")
}

func nsFlagFor(ns string) string {
	if ns == "" {
		return ""
	}
	return " -n " + shellQuote(ns)
}

// atoiDefault returns def when s doesn't parse as int.
func atoiDefault(s string, def int) int {
	n := 0
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return def
	}
	return n
}

func printLogsHelp() {
	fmt.Println(`用法: tssh logs <jump> <svc> [-n <ns>] [--tail N] [--since 5m] [-f|--no-follow]
      tssh logs <jump> -l <selector> ...

在 jump ECS 跑 kubectl logs 聚合多 pod 日志, 每行带 [ns/pod/container] 前缀.
相当于 stern/kubetail 但不用装.

选项:
  -n, --namespace <ns>   k8s namespace
  --tail <N>             每个 pod 拉最近 N 条 (默认 100, 0 表示不限)
  --since <dur>          只看最近 dur (如 5m / 1h)
  -l, --selector <sel>   直接给 label selector (如 app=grafana,env=prod)
                         不给就从 <svc> 反查 selector
  -f, --follow           流式追踪 (默认开启)
  --no-follow            只拉一次历史就退出

示例:
  tssh logs prod-jump grafana
  tssh logs prod-jump grafana -n monitoring --tail 200
  tssh logs prod-jump grafana --since 5m
  tssh logs prod-jump -l app=grafana,tier=frontend
  tssh logs prod-jump grafana --no-follow | grep ERROR     # 抓错误

实现: 在跳板用 kubectl logs --prefix -l <selector> -f, 通过 tssh
interactive session 流式传回本地. Ctrl-C 断开 session, kubectl 退出.`)
}
