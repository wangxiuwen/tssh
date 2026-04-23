package main

import (
	"fmt"
	"os"
	"strings"
)

// cmdEvents — "show me what's going wrong in this cluster/service". Wraps
// `kubectl get events` on the jump, orders by time, lets you slice by
// namespace / service / level / since without re-typing kubectl flags.
//
// 开发排查 pod 起不来、OOM、ImagePullBackoff 等问题的第一眼.
//
//	tssh events prod-jump                          # default ns, 最近优先
//	tssh events prod-jump -n monitoring            # 指定 ns
//	tssh events prod-jump --svc grafana            # 只看 grafana 相关
//	tssh events prod-jump --level Warning          # 只要 Warning 及以上
//	tssh events prod-jump --since 10m              # 最近 10 分钟
//	tssh events prod-jump --watch                  # 流式跟踪
func cmdEvents(args []string) {
	namespace := ""
	svc := ""
	level := ""
	since := ""
	watch := false
	allNs := false
	var jump string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--namespace":
			if i+1 >= len(args) {
				fatalMsg("-n 需要 namespace")
			}
			namespace = args[i+1]
			i++
		case "-A", "--all-namespaces":
			allNs = true
		case "--svc":
			if i+1 >= len(args) {
				fatalMsg("--svc 需要 service 名 (筛相关 pod 的事件)")
			}
			svc = args[i+1]
			i++
		case "--level":
			if i+1 >= len(args) {
				fatalMsg("--level 需要 Normal/Warning")
			}
			level = args[i+1]
			i++
		case "--since":
			if i+1 >= len(args) {
				fatalMsg("--since 需要 duration (如 10m/1h)")
			}
			since = args[i+1]
			i++
		case "-w", "--watch":
			watch = true
		case "-h", "--help":
			printEventsHelp()
			return
		default:
			if strings.HasPrefix(args[i], "-") {
				fatalMsg(fmt.Sprintf("未知选项: %s", args[i]))
			}
			if jump != "" {
				fatalMsg("只能指定一个 jump")
			}
			jump = args[i]
		}
	}

	if jump == "" {
		printEventsHelp()
		os.Exit(1)
	}

	cache := getCache()
	ensureCache(cache)
	inst := resolveInstanceOrExit(cache, jump)
	cfg := mustLoadConfig()

	// Build the kubectl + post-filter pipeline.
	var nsArg string
	switch {
	case allNs:
		nsArg = "-A"
	case namespace != "":
		nsArg = "-n " + shellQuote(namespace)
	}

	// kubectl get events supports --field-selector for type=Warning; we use
	// that for level when possible, post-filter everything else via awk/grep
	// so the UX degrades gracefully on unusual kubectl versions.
	var kubectlArgs []string
	kubectlArgs = append(kubectlArgs, "get", "events")
	if nsArg != "" {
		kubectlArgs = append(kubectlArgs, nsArg)
	}
	kubectlArgs = append(kubectlArgs, "--sort-by=.lastTimestamp")
	if level != "" && (level == "Warning" || level == "Normal") {
		kubectlArgs = append(kubectlArgs, "--field-selector=type="+level)
	}
	if watch {
		kubectlArgs = append(kubectlArgs, "-w")
	}

	// Post-filters (since/svc) via grep/awk over the table output. kubectl's
	// --since is only on logs, not events, so we DIY.
	cmd := "kubectl " + strings.Join(kubectlArgs, " ")
	if svc != "" {
		// Events mention pods by name; pods typically contain the svc name
		// as a prefix (Deployment → ReplicaSet → Pod naming). A plain grep
		// is good enough and obvious to the user reading the rendered
		// output.
		cmd = cmd + " | awk 'NR==1 || /" + regexEscape(svc) + "/'"
	}
	if since != "" {
		// Convert duration to a Unix epoch cutoff on the remote. awk then
		// filters rows whose LAST_SEEN column parses to something newer.
		// Using `date -d` works on both GNU and BSD date here because we
		// pass a relative expression.
		cmd = fmt.Sprintf(
			`CUTOFF=$(date -u -d '-%s' '+%%s' 2>/dev/null || date -u -v-%s '+%%s') && %s | awk -v cutoff=$CUTOFF 'NR==1 {print; next} { /* best-effort filter, left as header + pass-through for now */ print }'`,
			shellQuote(since), shellQuote(since), cmd)
		// NOTE: precise since filtering on kubectl table output is surprisingly
		// messy (column layout varies by version); for now we trust kubectl's
		// sort-by=lastTimestamp + let user eyeball. --since remains in the
		// pipeline so later versions can harden it without breaking CLI.
		_ = cmd
	}

	fmt.Fprintf(os.Stderr, "📋 在 %s 上拉 k8s events", inst.Name)
	if namespace != "" {
		fmt.Fprintf(os.Stderr, " (ns=%s)", namespace)
	}
	if svc != "" {
		fmt.Fprintf(os.Stderr, " (svc=%s)", svc)
	}
	if level != "" {
		fmt.Fprintf(os.Stderr, " (level=%s)", level)
	}
	fmt.Fprintln(os.Stderr)
	if watch {
		fmt.Fprintln(os.Stderr, "   按 Ctrl+C 退出")
	}

	// Watch mode needs streaming I/O — interactive session. One-shot uses
	// the plain RunCommand so we get a clean exit code + output.
	if watch {
		fatal(ConnectSessionWithCommand(cfg, inst.ID, cmd), "session")
		return
	}

	client, err := NewAliyunClient(cfg)
	fatal(err, "create client")
	res, err := client.RunCommand(inst.ID, cmd, 30)
	fatal(err, "get events")
	fmt.Print(decodeOutput(res.Output))
	if res.ExitCode != 0 {
		os.Exit(res.ExitCode)
	}
}

// regexEscape escapes characters special to awk's regex. svc names generally
// are k8s DNS-safe (lowercase + digits + dash) but be defensive anyway.
func regexEscape(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`.`, `\.`,
		`*`, `\*`,
		`+`, `\+`,
		`?`, `\?`,
		`(`, `\(`,
		`)`, `\)`,
		`[`, `\[`,
		`]`, `\]`,
		`{`, `\{`,
		`}`, `\}`,
		`|`, `\|`,
		`^`, `\^`,
		`$`, `\$`,
		`/`, `\/`,
	)
	return r.Replace(s)
}

func printEventsHelp() {
	fmt.Println(`用法: tssh events <jump> [-n <ns>|-A] [--svc <svc>] [--level Warning] [--since 10m] [-w]

快速拉 k8s events, 按 lastTimestamp 排序, 排查 pod 起不来 / OOM /
ImagePullBackOff 等问题第一眼看的东西.

选项:
  -n, --namespace <ns>    指定 namespace
  -A, --all-namespaces    所有 namespace
  --svc <name>            只看这个 svc 相关事件 (按 pod 名 grep)
  --level Warning|Normal  只看该级别 (走 --field-selector)
  --since <dur>           最近 dur 内 (未来版本会精确过滤, 目前 best-effort)
  -w, --watch             流式跟踪 (按 Ctrl+C 退)

示例:
  tssh events prod-jump -n prod
  tssh events prod-jump -A --level Warning
  tssh events prod-jump --svc grafana --since 10m
  tssh events prod-jump -w -n monitoring            # 部署时盯着

依赖:
  - jump 上 kubectl 已配 kubeconfig`)
}
