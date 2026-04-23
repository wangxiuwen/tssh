// Package k8s holds the tssh k8s-group subcommands: events (here), plus ks,
// kf, logs in future refactor steps. The whole package depends only on
// internal/core (for Runtime) and internal/shared (helpers) — never on
// cmd/tssh — so this code can be linked into a standalone `tssh-k8s`
// binary later without duplication.
package k8s

import (
	"fmt"
	"os"
	"strings"

	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/shared"
)

// Events wraps `kubectl get events` on the jump, orders by time, lets you
// slice by namespace / service / level / since without re-typing kubectl
// flags. First subcommand migrated to the group architecture.
func Events(rt core.Runtime, args []string) {
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
				shared.FatalMsg("-n 需要 namespace")
			}
			namespace = args[i+1]
			i++
		case "-A", "--all-namespaces":
			allNs = true
		case "--svc":
			if i+1 >= len(args) {
				shared.FatalMsg("--svc 需要 service 名 (筛相关 pod 的事件)")
			}
			svc = args[i+1]
			i++
		case "--level":
			if i+1 >= len(args) {
				shared.FatalMsg("--level 需要 Normal/Warning")
			}
			level = args[i+1]
			i++
		case "--since":
			if i+1 >= len(args) {
				shared.FatalMsg("--since 需要 duration (如 10m/1h)")
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
				shared.FatalMsg(fmt.Sprintf("未知选项: %s", args[i]))
			}
			if jump != "" {
				shared.FatalMsg("只能指定一个 jump")
			}
			jump = args[i]
		}
	}

	if jump == "" {
		printEventsHelp()
		os.Exit(1)
	}

	inst := rt.ResolveInstance(jump)
	if inst == nil {
		os.Exit(1)
	}

	var nsArg string
	switch {
	case allNs:
		nsArg = "-A"
	case namespace != "":
		nsArg = "-n " + shared.ShellQuote(namespace)
	}

	kubectlArgs := []string{"get", "events"}
	if nsArg != "" {
		kubectlArgs = append(kubectlArgs, nsArg)
	}
	kubectlArgs = append(kubectlArgs, "--sort-by=.lastTimestamp")
	if level != "" {
		// kubectl field-selector values are case-sensitive. Silently ignoring
		// `--level warning` was confusing — fail fast with an actionable message.
		if level != "Warning" && level != "Normal" {
			shared.FatalMsg(fmt.Sprintf("--level 只接受 Warning 或 Normal (区分大小写), 收到: %s", level))
		}
		kubectlArgs = append(kubectlArgs, "--field-selector=type="+level)
	}
	if watch {
		kubectlArgs = append(kubectlArgs, "-w")
	}

	cmd := "kubectl " + strings.Join(kubectlArgs, " ")
	if svc != "" {
		cmd = cmd + " | awk 'NR==1 || /" + regexEscape(svc) + "/'"
	}
	// --since: kubectl get events doesn't support it directly. Warn loudly
	// rather than silently ignoring — user expecting filtered output would
	// otherwise be puzzled why stale events come back.
	if since != "" {
		fmt.Fprintf(os.Stderr, "⚠️  --since %s: kubectl get events 不支持时间过滤, 已忽略 (请按 Age 列肉眼筛)\n", since)
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

	if watch {
		shared.Fatal(rt.ExecInteractive(inst.ID, cmd), "session")
		return
	}

	res, err := rt.ExecOneShot(inst.ID, cmd, 30)
	shared.Fatal(err, "get events")
	fmt.Print(res.Output)
	if res.ExitCode != 0 {
		os.Exit(res.ExitCode)
	}
}

// regexEscape — escape special chars for awk /…/ pattern.
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
  tssh events prod-jump -w -n monitoring`)
}
