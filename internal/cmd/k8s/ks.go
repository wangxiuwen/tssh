package k8s

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/shared"
)

// cmdKS — "look at this k8s service". Runs a single shell pipeline on a jump
// ECS that already has kubectl configured, collects pod health + exec-based
// netstat stats, and prints a summary. Intended for the "is my service alive
// and how loaded is it" moment, without the usual chain:
//
//	kubectl get pods -l ...
//	kubectl describe pod ...
//	kubectl top pod ...
//	kubectl exec ... -- ss -s
//
// Example:
//
//	tssh ks prod-jump grafana -n prod
//	tssh ks prod-jump grafana              (namespace default)
//	tssh ks prod-jump grafana -j           (JSON for scripting)
//
// We let kubectl fail loudly if the jump doesn't have a kubeconfig rather
// than trying to detect it ourselves — its error messages are actionable.
// KS runs a single shell pipeline on the jump to collect pod health +
// exec-based netstat stats for a k8s service, then prints a summary.
// (Renamed from cmdKS as part of the cmd/k8s extraction.)
func KS(rt core.Runtime, args []string) {
	namespace := ""
	jsonMode := false
	var positional []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--namespace":
			if i+1 >= len(args) {
				shared.FatalMsg("-n 需要 namespace")
			}
			namespace = args[i+1]
			i++
		case "-j", "--json":
			jsonMode = true
		case "-h", "--help":
			printKSHelp()
			return
		default:
			if strings.HasPrefix(args[i], "-") {
				shared.FatalMsg(fmt.Sprintf("未知选项: %s", args[i]))
			}
			positional = append(positional, args[i])
		}
	}

	if len(positional) < 2 {
		printKSHelp()
		os.Exit(1)
	}
	jump := positional[0]
	svc := positional[1]

	// Service name can come as "svc/foo" or just "foo"; strip the prefix.
	if idx := strings.Index(svc, "/"); idx >= 0 {
		svc = svc[idx+1:]
	}

	inst := rt.ResolveInstance(jump)
	if inst == nil {
		os.Exit(1)
	}

	nsFlag := ""
	if namespace != "" {
		nsFlag = "-n " + shared.ShellQuote(namespace)
	}

	// One script to rule them all — runs on the jump via Cloud Assistant.
	// Emits "key=value" lines plus per-pod blocks we parse below. Keeping
	// the protocol line-based (not JSON) makes it trivial to debug by hand.
	// ss inside pods needs either iproute2 or procps; we fall back to
	// /proc/net/tcp parsing so minimal images still work.
	//
	// Using placeholder substitution (not fmt.Sprintf) because the script
	// contains awk/printf %s sequences that Go vet misreads as format verbs.
	// Substitutions are wrapped in single quotes so ShellQuote-escaped content
	// (which may contain spaces or shell metacharacters) stays as a single
	// shell word. Without the quotes, `SVC=foo bar` would parse as "set SVC=foo
	// and run command bar".
	script := `#!/bin/bash
set -o pipefail
SVC='__SVC__'
NS='__NS__'
NSFLAG=__NSFLAG__
echo "svc=${SVC}"
echo "ns=${NS:-default}"

# Service + selector
if ! kubectl get svc "$SVC" $NSFLAG >/dev/null 2>&1; then
  echo "err=svc_not_found"
  exit 0
fi

SEL=$(kubectl get svc "$SVC" $NSFLAG -o jsonpath='{.spec.selector}' 2>/dev/null \
     | tr -d '{}"' | tr ',' ' ' | tr ':' '=')
echo "selector=${SEL}"

TYPE=$(kubectl get svc "$SVC" $NSFLAG -o jsonpath='{.spec.type}' 2>/dev/null)
CLUSTER_IP=$(kubectl get svc "$SVC" $NSFLAG -o jsonpath='{.spec.clusterIP}' 2>/dev/null)
PORTS=$(kubectl get svc "$SVC" $NSFLAG -o jsonpath='{range .spec.ports[*]}{.port}/{.protocol} {end}' 2>/dev/null)
echo "type=${TYPE}"
echo "cluster_ip=${CLUSTER_IP}"
echo "ports=${PORTS}"

# Endpoints (alive backends)
EP=$(kubectl get endpoints "$SVC" $NSFLAG -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null)
EP_COUNT=$(echo "$EP" | wc -w | tr -d ' ')
echo "endpoints_alive=${EP_COUNT}"

# Get pods via selector
LABELS=$(kubectl get svc "$SVC" $NSFLAG -o jsonpath='{.spec.selector}' 2>/dev/null \
        | tr -d '{}"' | tr ',' ',' | sed 's/:/=/g')
if [ -z "$LABELS" ]; then
  echo "err=no_selector"
  exit 0
fi

PODS_JSON=$(kubectl get pods $NSFLAG -l "$LABELS" \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.phase}{"\t"}{.status.containerStatuses[0].ready}{"\t"}{.status.podIP}{"\t"}{.spec.nodeName}{"\n"}{end}')

POD_TOTAL=0
POD_READY=0
echo "---pods---"
while IFS=$'\t' read -r PNAME PSTATUS PREADY PIP PNODE; do
  [ -z "$PNAME" ] && continue
  POD_TOTAL=$((POD_TOTAL + 1))
  [ "$PREADY" = "true" ] && POD_READY=$((POD_READY + 1))

  # ss -s inside the pod; if no ss, fall back to /proc/net/tcp counting.
  STATS=$(kubectl exec $NSFLAG "$PNAME" -- sh -c '
    if command -v ss >/dev/null 2>&1; then
      ss -s 2>/dev/null | awk "
        /estab/ { for(i=1;i<=NF;i++) if (\$i ~ /estab/) {print \"estab=\"\$(i-1); break} }
        /timewait/ { for(i=1;i<=NF;i++) if (\$i ~ /timewait/) {print \"tw=\"\$(i-1); break} }
      "
    else
      # /proc/net/tcp column 4 = state (hex): 01=ESTAB, 06=TIME_WAIT
      awk "NR>1 {print \$4}" /proc/net/tcp 2>/dev/null | sort | uniq -c | awk "
        \$2==\"01\" {printf \"estab=%s\n\",\$1}
        \$2==\"06\" {printf \"tw=%s\n\",\$1}
      "
    fi
  ' 2>/dev/null | tr '\n' '|')

  # kubectl top pod (cached via metrics-server; silent if unavailable)
  RES=$(kubectl top pod $NSFLAG "$PNAME" --no-headers 2>/dev/null | awk '{print "cpu="$2"|mem="$3}')

  echo "pod=${PNAME}|phase=${PSTATUS}|ready=${PREADY}|ip=${PIP}|node=${PNODE}|${STATS}${RES}"
done <<< "$PODS_JSON"

echo "---summary---"
echo "pod_total=${POD_TOTAL}"
echo "pod_ready=${POD_READY}"
`
	script = strings.ReplaceAll(script, "__SVC__", shared.ShellQuote(svc))
	script = strings.ReplaceAll(script, "__NSFLAG__", nsFlag)
	// __NS__ last because it could otherwise match fragments.
	script = strings.ReplaceAll(script, "__NS__", shared.ShellQuote(namespace))

	fmt.Fprintf(os.Stderr, "🔍 在 %s 查询 k8s service %s (ns=%s)...\n", inst.Name, svc, shared.DefaultStr(namespace, "default"))
	res, err := rt.ExecOneShot(inst.ID, script, 30)
	shared.Fatal(err, "run diag script")

	diag := parseKSOutput(res.Output)
	diag.Jump = inst.Name

	if jsonMode {
		b, _ := json.MarshalIndent(diag, "", "  ")
		fmt.Println(string(b))
		return
	}
	printKSResult(diag)
}

type ksPod struct {
	Name   string `json:"name"`
	Phase  string `json:"phase"`
	Ready  bool   `json:"ready"`
	IP     string `json:"ip"`
	Node   string `json:"node"`
	Estab  int    `json:"estab"`
	TW     int    `json:"timewait"`
	CPU    string `json:"cpu,omitempty"`
	Mem    string `json:"mem,omitempty"`
}

type ksDiag struct {
	Jump            string  `json:"jump"`
	Namespace       string  `json:"namespace"`
	Service         string  `json:"service"`
	Type            string  `json:"type,omitempty"`
	ClusterIP       string  `json:"cluster_ip,omitempty"`
	Ports           string  `json:"ports,omitempty"`
	Selector        string  `json:"selector,omitempty"`
	EndpointsAlive  int     `json:"endpoints_alive"`
	PodTotal        int     `json:"pod_total"`
	PodReady        int     `json:"pod_ready"`
	Pods            []ksPod `json:"pods,omitempty"`
	Err             string  `json:"err,omitempty"`
}

// parseKSOutput turns the remote script's key=value + per-pod lines into a
// struct. Format chosen to be simple grep-parseable and robust to missing
// tools on the pod.
func parseKSOutput(out string) ksDiag {
	var d ksDiag
	d.Namespace = "default"
	podRe := regexp.MustCompile(`pod=([^|]+)\|phase=([^|]*)\|ready=([^|]*)\|ip=([^|]*)\|node=([^|]*)(?:\|(.*))?`)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := podRe.FindStringSubmatch(line); m != nil {
			pod := ksPod{
				Name:  m[1],
				Phase: m[2],
				Ready: m[3] == "true",
				IP:    m[4],
				Node:  m[5],
			}
			// Parse trailing stats key=value pipe-separated.
			if len(m) >= 7 {
				for _, kv := range strings.Split(m[6], "|") {
					kv = strings.TrimSpace(kv)
					if kv == "" {
						continue
					}
					eq := strings.IndexByte(kv, '=')
					if eq <= 0 {
						continue
					}
					k, v := kv[:eq], kv[eq+1:]
					switch k {
					case "estab":
						pod.Estab, _ = strconv.Atoi(v)
					case "tw":
						pod.TW, _ = strconv.Atoi(v)
					case "cpu":
						pod.CPU = v
					case "mem":
						pod.Mem = v
					}
				}
			}
			d.Pods = append(d.Pods, pod)
			continue
		}
		if eq := strings.IndexByte(line, '='); eq > 0 {
			k, v := line[:eq], line[eq+1:]
			switch k {
			case "svc":
				d.Service = v
			case "ns":
				d.Namespace = v
			case "type":
				d.Type = v
			case "cluster_ip":
				d.ClusterIP = v
			case "ports":
				d.Ports = strings.TrimSpace(v)
			case "selector":
				d.Selector = strings.TrimSpace(v)
			case "endpoints_alive":
				d.EndpointsAlive, _ = strconv.Atoi(v)
			case "pod_total":
				d.PodTotal, _ = strconv.Atoi(v)
			case "pod_ready":
				d.PodReady, _ = strconv.Atoi(v)
			case "err":
				d.Err = v
			}
		}
	}
	return d
}

func printKSResult(d ksDiag) {
	if d.Err == "svc_not_found" {
		fmt.Fprintf(os.Stderr, "❌ service %s 在 namespace %s 找不到\n", d.Service, d.Namespace)
		os.Exit(1)
	}
	if d.Err == "no_selector" {
		fmt.Fprintf(os.Stderr, "⚠️  service %s 没有 selector (可能是 ExternalName/headless), 无法定位 pod\n", d.Service)
	}

	fmt.Println()
	fmt.Printf("📦 Service  %s  (ns: %s, type: %s)\n", d.Service, d.Namespace, d.Type)
	fmt.Printf("   ClusterIP  %s   Ports: %s\n", d.ClusterIP, d.Ports)
	fmt.Printf("   Selector   %s\n", d.Selector)
	fmt.Printf("   Endpoints  %d alive\n", d.EndpointsAlive)
	fmt.Printf("   Pods       %d/%d ready\n", d.PodReady, d.PodTotal)
	fmt.Println()

	if len(d.Pods) == 0 {
		fmt.Println("   (没有 pod)")
		return
	}

	fmt.Printf("   %-30s %-10s %-5s %-14s %8s %8s %8s %8s\n",
		"POD", "PHASE", "READY", "POD_IP", "CPU", "MEM", "ESTAB", "TW")
	for _, p := range d.Pods {
		ready := "no"
		if p.Ready {
			ready = "yes"
		}
		fmt.Printf("   %-30s %-10s %-5s %-14s %8s %8s %8d %8d\n",
			shared.TruncateStr(p.Name, 30), p.Phase, ready, p.IP,
			shared.DefaultStr(p.CPU, "-"), shared.DefaultStr(p.Mem, "-"),
			p.Estab, p.TW)
	}
	fmt.Println()

	// Aggregate totals since these are the "how loaded is the service" signal.
	var totEstab, totTW int
	for _, p := range d.Pods {
		totEstab += p.Estab
		totTW += p.TW
	}
	fmt.Printf("   总连接数    ESTAB=%d   TIME_WAIT=%d\n", totEstab, totTW)
	if totTW > totEstab*3 && totTW > 100 {
		fmt.Println("   ⚠️  TIME_WAIT 偏多, 可能有短连接滥用 (建议加连接池或长连接)")
	}
}

// (defaultStr moved to internal/shared.DefaultStr)

func printKSHelp() {
	fmt.Println(`用法: tssh ks <jump-ecs> <service> [-n <namespace>] [-j]

在 jump ECS 上跑 kubectl 聚合, 一眼看 k8s service 的运行状态.

输出:
  - Service 基本信息 (type, ClusterIP, ports, selector)
  - Endpoints 可用数 (活着的 backend pod IP 数)
  - 每个 pod 的: phase, ready, pod IP, node, CPU, MEM, ESTAB, TIME_WAIT
  - 总连接数聚合; TIME_WAIT 过多时警告

依赖 (跳板 ECS 上):
  - kubectl 已配置 kubeconfig, 能访问目标集群
  - 各 pod 里要么有 ss (iproute2), 要么有可读的 /proc/net/tcp
  - kubectl top 依赖 metrics-server (没有就 CPU/MEM 留空)

示例:
  tssh ks prod-jump grafana                     # namespace: default
  tssh ks prod-jump svc/grafana -n monitoring   # 显式 namespace
  tssh ks prod-jump grafana -j                  # JSON 给脚本

JSON 字段:
  {"jump":"...","namespace":"...","service":"...","type":"ClusterIP",
   "cluster_ip":"...","ports":"80/TCP","selector":"app=grafana",
   "endpoints_alive":3,"pod_total":3,"pod_ready":3,
   "pods":[{"name":"...","phase":"Running","ready":true,"ip":"...",
            "node":"...","estab":42,"timewait":120,"cpu":"15m","mem":"80Mi"}]}`)
}
