package k8s

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/shared"
)

// cmdKF — "kubectl port-forward but for all my services at once". Runs
// kubectl port-forward in the background on a jump ECS (one per target)
// then local-forwards each of those ports to this machine. Matches the
// "I don't want to `kubectl port-forward` 5 times to click around"
// complaint.
//
//	tssh kf prod-jump grafana:80
//	tssh kf prod-jump -n monitoring grafana:80 prometheus:9090
//	tssh kf prod-jump grafana:80=3000             固定本地 3000
//	tssh kf prod-jump grafana:80 --browser        同时拉起 Chrome
//
// 不替换 kubectl: kubectl 本身仍是工具, kf 只是 "开多个 & 自动清理 & 本地可达"
// 的包装.
// KF — renamed from cmdKF as part of the cmd/k8s extraction.
func KF(rt core.Runtime, args []string) {
	namespace := ""
	openBrowser := false
	jsonMode := false
	var jump string
	var rawTargets []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--namespace":
			if i+1 >= len(args) {
				shared.FatalMsg("-n 需要 namespace")
			}
			namespace = args[i+1]
			i++
		case "--browser":
			openBrowser = true
		case "-j", "--json":
			jsonMode = true
		case "-h", "--help":
			printKFHelp()
			return
		default:
			if strings.HasPrefix(args[i], "-") {
				shared.FatalMsg(fmt.Sprintf("未知选项: %s", args[i]))
			}
			if jump == "" {
				jump = args[i]
			} else {
				rawTargets = append(rawTargets, args[i])
			}
		}
	}

	if jump == "" || len(rawTargets) == 0 {
		printKFHelp()
		os.Exit(1)
	}

	targets, err := parseKFTargets(rawTargets)
	shared.Fatal(err, "parse targets")

	inst := rt.ResolveInstance(jump)
	if inst == nil {
		os.Exit(1)
	}

	nsFlag := ""
	if namespace != "" {
		nsFlag = " -n " + shared.ShellQuote(namespace)
	}

	// One remote shell session per target. kubectl port-forward runs
	// --address 127.0.0.1 so it's only reachable via our Cloud Assistant
	// tunnel, not the VPC.
	var cleanups []func()
	rollback := func() {
		for _, c := range cleanups {
			c()
		}
	}

	for i := range targets {
		if targets[i].remotePort == 0 {
			// Remote kubectl listener port — picks something unlikely to
			// collide with other kf runs. Range matches fwd/socat.
			targets[i].remotePort = shared.FindFreePortInRange(18000, 18999)
		}
		startCmd := fmt.Sprintf(
			"nohup kubectl%s port-forward --address 127.0.0.1 svc/%s %d:%d >/tmp/tssh-kf-%d.log 2>&1 & echo $!",
			nsFlag, shared.ShellQuote(targets[i].svc), targets[i].remotePort, targets[i].svcPort, targets[i].remotePort,
		)
		res, err := rt.ExecOneShot(inst.ID, startCmd, 15)
		if err != nil {
			rollback()
			shared.Fatal(err, fmt.Sprintf("kubectl port-forward for %s", targets[i].svc))
		}
		pid := strings.TrimSpace(res.Output)
		if _, perr := strconv.Atoi(pid); perr != nil {
			rollback()
			shared.FatalMsg(fmt.Sprintf("启动 kubectl 失败 (svc=%s): 可能 kubectl 未安装或 kubeconfig 没配", targets[i].svc))
		}
		targets[i].remotePID = pid

		// Remote kill on cleanup.
		pidCopy := pid
		cleanups = append(cleanups, func() {
			_, _ = rt.ExecOneShot(inst.ID, fmt.Sprintf("kill %s 2>/dev/null", shared.ShellQuote(pidCopy)), 5)
		})

		// Allocate local port + start local->remote tunnel.
		if targets[i].localPort == 0 {
			targets[i].localPort = shared.FindFreePort()
		}
		stop, err := rt.StartPortForward(inst.ID, targets[i].localPort, targets[i].remotePort)
		if err != nil {
			rollback()
			shared.Fatal(err, fmt.Sprintf("port-forward for %s", targets[i].svc))
		}
		cleanups = append(cleanups, stop)
	}

	// Output — JSON for agents, emoji for humans. Browser mode is a nice
	// extra for Web UIs; tssh browser handles its own profile/proxy stuff.
	if jsonMode {
		type entry struct {
			Svc        string `json:"svc"`
			SvcPort    int    `json:"svc_port"`
			LocalPort  int    `json:"local_port"`
			URL        string `json:"url"`
			RemotePID  string `json:"remote_pid"`
		}
		var entries []entry
		for _, t := range targets {
			entries = append(entries, entry{
				Svc: t.svc, SvcPort: t.svcPort, LocalPort: t.localPort,
				URL: fmt.Sprintf("http://127.0.0.1:%d", t.localPort),
				RemotePID: t.remotePID,
			})
		}
		b, _ := json.Marshal(map[string]interface{}{
			"jump":      inst.Name,
			"jump_id":   inst.ID,
			"namespace": namespace,
			"targets":   entries,
			"pid":       os.Getpid(),
		})
		fmt.Println(string(b))
		os.Stdout.Sync()
	} else {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "🔌 k8s port-forward 就绪 via %s (ns=%s)\n", inst.Name, shared.DefaultStr(namespace, "default"))
		for _, t := range targets {
			fmt.Fprintf(os.Stderr, "   http://127.0.0.1:%d  →  svc/%s:%d\n", t.localPort, t.svc, t.svcPort)
		}
		fmt.Fprintln(os.Stderr, "   按 Ctrl+C 退出, 会清远端 kubectl + 本地转发")
	}

	if openBrowser {
		// Spawn `tssh browser` as a child pointed at the local ports.
		// We pass the jump's own name so it reuses the same SOCKS5 profile
		// directory (登录状态保留); but actually for local kubectl-forwarded
		// ports we don't need a SOCKS5 — localhost is enough. Use the
		// system default browser instead.
		for _, t := range targets {
			_ = openURL(fmt.Sprintf("http://127.0.0.1:%d", t.localPort))
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	if !jsonMode {
		fmt.Fprintln(os.Stderr, "\n🛑 清理中...")
	}
	rollback()
}

// kfTarget: one entry on tssh kf's command line.
type kfTarget struct {
	svc        string // kubectl svc name (supports svc/foo prefix stripping)
	svcPort    int    // port on the service
	localPort  int    // optional user-specified local port
	remotePort int    // kubectl listen port on the jump (allocated)
	remotePID  string // kubectl's remote PID
}

// parseKFTargets parses "svc:port" or "svc:port=localPort".
func parseKFTargets(raw []string) ([]kfTarget, error) {
	var out []kfTarget
	for _, r := range raw {
		t, err := parseKFTarget(r)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func parseKFTarget(r string) (kfTarget, error) {
	// Optional local-port override via "=<port>" suffix.
	var localPort int
	if eq := strings.IndexByte(r, '='); eq >= 0 {
		lp, err := strconv.Atoi(r[eq+1:])
		if err != nil || lp <= 0 || lp > 65535 {
			return kfTarget{}, fmt.Errorf("invalid local port in %q", r)
		}
		localPort = lp
		r = r[:eq]
	}
	// Strip any svc/ / svcname/ prefix — for now kubectl port-forward always
	// takes svc/<name>, but we accept bare names too for ergonomics.
	if idx := strings.IndexByte(r, '/'); idx >= 0 {
		r = r[idx+1:]
	}
	colon := strings.LastIndex(r, ":")
	if colon <= 0 || colon == len(r)-1 {
		return kfTarget{}, fmt.Errorf("target %q 应为 <svc>:<port>", r)
	}
	sp, err := strconv.Atoi(r[colon+1:])
	if err != nil || sp <= 0 || sp > 65535 {
		return kfTarget{}, fmt.Errorf("invalid svc port in %q", r)
	}
	return kfTarget{svc: r[:colon], svcPort: sp, localPort: localPort}, nil
}

// openURL launches the default browser on the host. Best-effort; a failure
// here shouldn't block the tssh kf tunnels from staying up.
func openURL(url string) error {
	candidates := []string{"open", "xdg-open"}
	for _, bin := range candidates {
		if path, err := exec.LookPath(bin); err == nil {
			return shared.ExecCommand(path, url).Start()
		}
	}
	fmt.Fprintf(os.Stderr, "ℹ️  请手动打开: %s\n", url)
	return nil
}

func printKFHelp() {
	fmt.Println(`用法: tssh kf <jump> [-n <ns>] <svc:port>[=<localPort>] [<svc:port>...] [--browser] [-j]

在 jump ECS 上批量起 kubectl port-forward, 每个 svc 自动分配本地端口
(或手动指定), 让本地能直接访问内网 k8s service.

target 格式:
  grafana:80              svc/grafana 的 80 端口 → 自动分配本地端口
  grafana:80=3000         固定本地 3000 → svc/grafana:80
  svc/grafana:80          显式 svc/ 前缀也接受

选项:
  -n, --namespace <ns>    k8s namespace (默认: default)
  --browser               起完之后用系统默认浏览器打开每个 URL
  -j, --json              stdout 一行 JSON (AI/脚本)

依赖 (跳板 ECS 上):
  - kubectl 已配置 kubeconfig, 能访问目标集群

示例:
  tssh kf prod-jump grafana:80                              # 一个 svc
  tssh kf prod-jump -n monitoring grafana:80 prometheus:9090 # 多 svc
  tssh kf prod-jump grafana:80=3000 --browser               # 固定端口 + 开浏览器

Ctrl+C 退出会自动:
  1. kill 远端 kubectl port-forward
  2. 关本地 tunnel

JSON 输出:
  {"jump":"prod-jump","jump_id":"i-...","namespace":"monitoring",
   "targets":[{"svc":"grafana","svc_port":80,"local_port":54321,
               "url":"http://127.0.0.1:54321","remote_pid":"1234"}, ...],
   "pid":5678}`)
}
