package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// cmdArmsTrace dispatches on flags:
//   - positional TraceID → GetTrace (show spans)
//   - --globalId <v>     → SearchTraces with tag globalId=<v>
//   - --tag k=v (repeat) → SearchTraces with arbitrary tags
//
// Additional flags: --pid, --op, --ip, --since <dur>, --limit, -j/--json
func cmdArmsTrace(args []string) {
	jsonMode := false
	var traceID string
	var pid, opName, serviceIP string
	var since = time.Hour
	var limit = 50
	tags := map[string]string{}

	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "❌ %s 需要一个参数\n", a)
				os.Exit(1)
			}
			i++
			return args[i]
		}
		switch a {
		case "-j", "--json":
			jsonMode = true
		case "-h", "--help":
			printTraceHelp()
			return
		case "--globalId", "-g":
			tags["globalId"] = next()
		case "--tag", "-t":
			kv := next()
			k, v, ok := strings.Cut(kv, "=")
			if !ok || k == "" {
				fmt.Fprintf(os.Stderr, "❌ --tag 需要 key=value 形式, 收到: %s\n", kv)
				os.Exit(1)
			}
			tags[k] = v
		case "--pid":
			pid = next()
		case "--op", "--operation":
			opName = next()
		case "--ip":
			serviceIP = next()
		case "--since":
			d, err := time.ParseDuration(next())
			if err != nil {
				fmt.Fprintf(os.Stderr, "❌ --since 无法解析: %v\n", err)
				os.Exit(1)
			}
			since = d
		case "--limit":
			if _, err := fmt.Sscanf(next(), "%d", &limit); err != nil || limit <= 0 {
				fmt.Fprintf(os.Stderr, "❌ --limit 需要正整数\n")
				os.Exit(1)
			}
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "❌ 未知选项: %s\n", a)
				printTraceHelp()
				os.Exit(1)
			}
			if traceID != "" {
				fmt.Fprintf(os.Stderr, "❌ 只能指定一个 TraceID\n")
				os.Exit(1)
			}
			traceID = a
		}
	}

	if traceID == "" && len(tags) == 0 {
		printTraceHelp()
		return
	}

	client := mustARMSClient()

	if traceID != "" {
		spans, err := client.GetTrace(traceID)
		fatal(err, "GetTrace")
		if jsonMode {
			data, _ := json.MarshalIndent(spans, "", "  ")
			fmt.Println(string(data))
			return
		}
		printTraceSpans(traceID, spans)
		return
	}

	// Search mode
	now := time.Now()
	opts := TraceSearchOptions{
		Pid:           pid,
		OperationName: opName,
		ServiceIp:     serviceIP,
		Tags:          tags,
		StartMs:       now.Add(-since).UnixMilli(),
		EndMs:         now.UnixMilli(),
		PageSize:      limit,
		CurrentPage:   1,
	}
	traces, total, err := client.SearchTraces(opts)
	fatal(err, "SearchTraces")

	if jsonMode {
		data, _ := json.MarshalIndent(traces, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(traces) == 0 {
		fmt.Fprintf(os.Stderr, "未找到匹配的 trace (过去 %s, 过滤: %s)\n", since, formatTags(tags))
		os.Exit(1)
	}

	// Exactly one match → auto-expand into spans
	if len(traces) == 1 {
		tr := traces[0]
		fmt.Fprintf(os.Stderr, "🔎 匹配到 1 条 trace: %s (%s)\n", tr.TraceID, tr.ServiceName)
		spans, err := client.GetTrace(tr.TraceID)
		fatal(err, "GetTrace")
		printTraceSpans(tr.TraceID, spans)
		return
	}

	// Multiple matches → summary table
	fmt.Printf("🔎 匹配 %d 条 trace (显示前 %d)\n", total, len(traces))
	printTraceSummary(traces)
}

func printTraceSummary(traces []TraceInfo) {
	w := newTabWriter()
	fmt.Fprintf(w, "#\tTime\tDuration\tService\tIP\tOperation\tTraceID\n")
	for i, t := range traces {
		ts := time.UnixMilli(t.Timestamp).Format("15:04:05")
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1, ts, formatMs(t.Duration), t.ServiceName, t.ServiceIp,
			truncateStr(t.OperationName, 40), t.TraceID)
	}
	w.Flush()
	fmt.Fprintln(os.Stderr, "\n提示: tssh arms trace <TraceID> 查看完整 span 列表")
}

func printTraceSpans(traceID string, spans []TraceSpan) {
	if len(spans) == 0 {
		fmt.Fprintf(os.Stderr, "未找到 trace %s 的 span\n", traceID)
		os.Exit(1)
	}
	// Sort by RpcId (hierarchical ID) for a readable call tree.
	sort.Slice(spans, func(i, j int) bool {
		return spans[i].RpcID < spans[j].RpcID
	})

	fmt.Printf("🧵 Trace %s — %d spans\n", traceID, len(spans))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Compute max depth for visual indent budget.
	for _, s := range spans {
		depth := strings.Count(s.RpcID, ".")
		indent := strings.Repeat("  ", depth)
		icon := spanIcon(s)
		fmt.Printf("%s%s %s %s  %s\n",
			indent, icon, s.RpcID, formatMs(s.Duration), s.OperationName)
		fmt.Printf("%s    %s @ %s\n", indent, s.ServiceName, s.ServiceIp)
		if s.ResultCode != "" && s.ResultCode != "00" && s.ResultCode != "SUCCESS" {
			fmt.Printf("%s    ❌ result=%s\n", indent, s.ResultCode)
		}
		for _, tag := range s.TagEntryList {
			if isInterestingTag(tag.Key) {
				fmt.Printf("%s    %s=%s\n", indent, tag.Key, truncateStr(tag.Value, 120))
			}
		}
	}
}

func spanIcon(s TraceSpan) string {
	if s.ResultCode != "" && s.ResultCode != "00" && s.ResultCode != "SUCCESS" {
		return "❌"
	}
	switch s.RpcType {
	case 0:
		return "🌐" // HTTP entry
	case 1:
		return "🔗" // outgoing HTTP
	case 2:
		return "🗄️ " // DB
	case 3:
		return "📨" // MQ
	default:
		return "▸"
	}
}

// Filter: show tags that tend to matter; hide the long tail of platform tags.
var interestingSpanTags = map[string]bool{
	"globalId":      true,
	"http.url":      true,
	"http.method":   true,
	"http.status":   true,
	"db.statement":  true,
	"db.instance":   true,
	"mq.topic":      true,
	"error":         true,
	"error.message": true,
	"exception":     true,
	"rpc.type":      true,
	"userId":        true,
	"requestId":     true,
}

func isInterestingTag(k string) bool {
	if interestingSpanTags[k] {
		return true
	}
	// Heuristic: any tag ending with "Id" or containing "error"
	return strings.HasSuffix(k, "Id") || strings.Contains(strings.ToLower(k), "error")
}

func formatMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.2fs", float64(ms)/1000)
}

func formatTags(tags map[string]string) string {
	parts := make([]string, 0, len(tags))
	for k, v := range tags {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ", ")
}

func printTraceHelp() {
	fmt.Println(`用法: tssh arms trace <TraceID>
       tssh arms trace --globalId <value> [过滤选项]
       tssh arms trace --tag key=value [--tag k=v ...] [过滤选项]

查看模式 (按 TraceID):
  tssh arms trace 0a1b2c3d4e5f...      拉取并打印所有 span (调用树)

搜索模式 (按 tag):
  -g, --globalId <v>       globalId 查询 (= --tag globalId=<v>)
  -t, --tag k=v            自定义 tag 过滤 (可重复)

过滤选项:
  --pid <pid>              限定 ARMS 应用 PID
  --op <name>              按 OperationName 过滤
  --ip <ip>                按 ServiceIp 过滤
  --since <dur>            时间窗口 (默认 1h, 如 30m / 2h / 24h)
  --limit <n>              最多返回 n 条 (默认 50)

通用:
  -j, --json               JSON 输出

示例:
  tssh arms trace --globalId req-abc-123
  tssh arms trace --tag userId=42 --since 2h --limit 20
  tssh arms trace 0a1b2c3d4e5f6708091a2b3c4d5e6f70`)
}
