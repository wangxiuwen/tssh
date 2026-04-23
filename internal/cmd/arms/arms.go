package arms

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/wangxiuwen/tssh/internal/aliyun"
	"github.com/wangxiuwen/tssh/internal/config"
	"github.com/wangxiuwen/tssh/internal/core"
	"github.com/wangxiuwen/tssh/internal/grafana"
	"github.com/wangxiuwen/tssh/internal/shared"
)

// appRuntime is set by Arms() so helpers like mustARMSClient can grab config
// via the core.Runtime contract, same as other groups.
var appRuntime core.Runtime

// Arms is the entry point wired into cmd/tssh and future cmd/tssh-arms.
func Arms(rt core.Runtime, args []string) {
	appRuntime = rt
	armsGroup.Dispatch(args)
}

var armsGroup = shared.CmdGroup{
	Name:    "arms",
	Desc:    "ARMS 监控: 告警、仪表盘、Prometheus 查询、Trace",
	Default: cmdArmsAlerts,
	Commands: []shared.SubCmd{
		{Name: "alerts", Desc: "查看当前触发中的告警 [-j]", Run: cmdArmsAlerts},
		{Name: "dash", Desc: "列出/搜索仪表盘 [keyword] [-j]", Run: cmdArmsDash},
		{Name: "ds", Desc: "列出数据源 [-j]", Run: cmdArmsDs},
		{Name: "open", Desc: "浏览器打开仪表盘 [keyword]", Run: cmdArmsOpen},
		{Name: "query", Desc: "Prometheus 查询 <promql|shortcut> [-j]", Run: cmdArmsQuery},
		{Name: "trace", Desc: "查看/搜索 Trace: <traceID> | --globalId <v> | --tag k=v", Run: cmdArmsTrace},
	},
}

// mustGrafanaClient creates a Grafana client.
// Priority: explicit Grafana config → auto-discover from Aliyun ARMS API
func mustGrafanaClient() *grafana.Client {
	cfg, err := config.LoadGrafana()
	if err == nil {
		return grafana.NewClient(cfg)
	}

	// Fallback: auto-discover via Aliyun credentials
	aliyunCfg := appRuntime.LoadConfig()
	armsClient, err2 := aliyun.NewARMSClient(aliyunCfg)
	if err2 != nil {
		// Show the original Grafana config error
		shared.Fatal(err, "load grafana config")
	}

	discovered, err2 := armsClient.DiscoverGrafanaConfig()
	if err2 != nil {
		fmt.Fprintf(os.Stderr, "⚠️  自动发现 Grafana 失败: %v\n", err2)
		shared.Fatal(err, "load grafana config")
	}

	fmt.Fprintf(os.Stderr, "📡 自动发现 Grafana: %s\n", discovered.Endpoint)
	fmt.Fprintf(os.Stderr, "⚠️  Grafana 仪表盘/查询功能需要 API token\n")
	fmt.Fprintf(os.Stderr, "   在 Grafana 控制台创建 Service Account token，然后:\n")
	fmt.Fprintf(os.Stderr, "   export TSSH_GRAFANA_TOKEN=glsa_xxx\n")
	fmt.Fprintf(os.Stderr, "   或在 ~/.tssh/config.json 中配置 grafana.token\n")
	os.Exit(1)
	return grafana.NewClient(discovered)
}

// mustARMSClient creates an ARMS API client from Aliyun credentials
func mustARMSClient() *aliyun.ARMSClient {
	cfg := appRuntime.LoadConfig()
	client, err := aliyun.NewARMSClient(cfg)
	shared.Fatal(err, "create ARMS client")
	return client
}

// cmdArmsAlerts shows currently firing alerts.
// Uses ARMS API directly (no Grafana token needed).
func cmdArmsAlerts(args []string) {
	jsonMode := hasFlag(args, "-j", "--json")

	armsClient := mustARMSClient()
	alerts, err := armsClient.FetchAllActivatedAlerts()
	shared.Fatal(err, "fetch alerts")

	if jsonMode {
		data, _ := json.MarshalIndent(alerts, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(alerts) == 0 {
		fmt.Println("✅ 当前无触发中的告警")
		return
	}

	// Sort by severity
	sort.Slice(alerts, func(i, j int) bool {
		return severityOrder(alerts[i].Severity) < severityOrder(alerts[j].Severity)
	})

	fmt.Printf("🔥 当前告警 (%d 个触发中)\n", len(alerts))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	for _, alert := range alerts {
		printActivatedAlert(alert)
	}
}

func printActivatedAlert(alert aliyun.ActivatedAlert) {
	severity := alert.Severity
	if severity == "" {
		severity = alert.ExpandFields["severity"]
	}
	icon := severityIcon(severity)

	fmt.Printf("%s [%s] %s\n", icon, severity, alert.AlertName)

	if alert.IntegrationType != "" {
		fmt.Printf("   来源: %s\n", alert.IntegrationName)
	}

	// Show expand fields (key details)
	for _, k := range []string{"alertname", "instance", "service", "host"} {
		if v, ok := alert.ExpandFields[k]; ok && k != "alertname" {
			fmt.Printf("   %s: %s\n", k, v)
		}
	}

	if alert.StartsAt > 0 {
		startTime := time.Unix(alert.StartsAt/1000, 0)
		dur := time.Since(startTime)
		fmt.Printf("   持续: %s\n", shared.FormatDuration(dur))
	}

	fmt.Println()
}

func printAlert(alert grafana.Alert) {
	severity := alert.Labels["severity"]
	alertname := alert.Labels["alertname"]
	icon := severityIcon(severity)

	fmt.Printf("%s [%s] %s\n", icon, severity, alertname)

	// Show key labels (exclude alertname and severity as they're already shown)
	for k, v := range alert.Labels {
		if k == "alertname" || k == "severity" || k == "grafana_folder" {
			continue
		}
		fmt.Printf("   %s: %s\n", k, v)
	}

	// Show annotations (summary, description)
	if s := alert.Annotations["summary"]; s != "" {
		fmt.Printf("   摘要: %s\n", s)
	}
	if s := alert.Annotations["description"]; s != "" {
		fmt.Printf("   描述: %s\n", s)
	}

	// Duration
	if !alert.StartsAt.IsZero() {
		dur := time.Since(alert.StartsAt)
		fmt.Printf("   持续: %s\n", shared.FormatDuration(dur))
	}

	fmt.Println()
}

func severityOrder(s string) int {
	switch strings.ToLower(s) {
	case "critical":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}

func severityIcon(s string) string {
	switch strings.ToLower(s) {
	case "critical":
		return "🔴"
	case "warning":
		return "🟡"
	case "info":
		return "🔵"
	default:
		return "⚪"
	}
}

func hasFlag(args []string, flags ...string) bool {
	for _, arg := range args {
		for _, f := range flags {
			if arg == f {
				return true
			}
		}
	}
	return false
}

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
}

func getNonFlagArg(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}
	return ""
}

// cmdArmsDash lists or searches Grafana dashboards
func cmdArmsDash(args []string) {
	jsonMode := hasFlag(args, "-j", "--json")
	query := getNonFlagArg(args)

	client := mustGrafanaClient()
	dashboards, err := client.SearchDashboards(query)
	shared.Fatal(err, "search dashboards")

	if jsonMode {
		data, _ := json.MarshalIndent(dashboards, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(dashboards) == 0 {
		if query != "" {
			fmt.Fprintf(os.Stderr, "未找到匹配 '%s' 的仪表盘\n", query)
		} else {
			fmt.Println("没有仪表盘")
		}
		return
	}

	w := newTabWriter()
	fmt.Fprintf(w, "#\t名称\t标签\t文件夹\n")
	for i, d := range dashboards {
		tags := strings.Join(d.Tags, ",")
		folder := d.FolderTitle
		if folder == "" {
			folder = "-"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", i+1, d.Title, tags, folder)
	}
	w.Flush()
	fmt.Fprintf(os.Stderr, "\n共 %d 个仪表盘\n", len(dashboards))
}

// cmdArmsDs lists Grafana data sources
func cmdArmsDs(args []string) {
	jsonMode := hasFlag(args, "-j", "--json")

	client := mustGrafanaClient()
	datasources, err := client.FetchDatasources()
	shared.Fatal(err, "fetch datasources")

	if jsonMode {
		data, _ := json.MarshalIndent(datasources, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(datasources) == 0 {
		fmt.Println("没有数据源")
		return
	}

	w := newTabWriter()
	fmt.Fprintf(w, "#\tID\t名称\t类型\n")
	for i, ds := range datasources {
		fmt.Fprintf(w, "%d\t%d\t%s\t%s\n", i+1, ds.ID, ds.Name, ds.Type)
	}
	w.Flush()
	fmt.Fprintf(os.Stderr, "\n共 %d 个数据源\n", len(datasources))
}

// cmdArmsOpen opens a Grafana dashboard in the browser
func cmdArmsOpen(args []string) {
	query := getNonFlagArg(args)

	client := mustGrafanaClient()
	dashboards, err := client.SearchDashboards(query)
	shared.Fatal(err, "search dashboards")

	if len(dashboards) == 0 {
		fmt.Fprintln(os.Stderr, "未找到仪表盘")
		os.Exit(1)
	}

	var target grafana.Dashboard
	if len(dashboards) == 1 {
		target = dashboards[0]
	} else {
		// Multiple matches — let user choose
		fmt.Fprintf(os.Stderr, "匹配到 %d 个仪表盘，请选择:\n", len(dashboards))
		for i, d := range dashboards {
			fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, d.Title)
		}
		fmt.Fprint(os.Stderr, "输入编号: ")
		var choice int
		if _, err := fmt.Scan(&choice); err != nil || choice < 1 || choice > len(dashboards) {
			fmt.Fprintln(os.Stderr, "无效选择")
			os.Exit(1)
		}
		target = dashboards[choice-1]
	}

	url := client.DashboardURL(target)
	fmt.Fprintf(os.Stderr, "🔗 打开: %s\n", target.Title)
	openBrowser(url)
}

func openBrowser(url string) {
	var cmd string
	switch {
	case fileExists("/usr/bin/open"):
		cmd = "open"
	case fileExists("/usr/bin/xdg-open"):
		cmd = "xdg-open"
	default:
		fmt.Printf("请手动打开: %s\n", url)
		return
	}
	exec := shared.ExecCommand(cmd, url)
	exec.Start()
}

func fileExists(path string) bool { return shared.FileExists(path) }

// cmdArmsQuery executes a Prometheus query via Grafana proxy
func cmdArmsQuery(args []string) {
	jsonMode := hasFlag(args, "-j", "--json")

	// Parse -d <dsID> flag
	dsID := 0
	var remaining []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-d" && i+1 < len(args) {
			fmt.Sscanf(args[i+1], "%d", &dsID)
			i++
		} else if args[i] == "-j" || args[i] == "--json" {
			continue
		} else {
			remaining = append(remaining, args[i])
		}
	}

	if len(remaining) == 0 {
		printQueryHelp()
		return
	}

	client := mustGrafanaClient()

	query := strings.Join(remaining, " ")

	// Built-in shortcut queries — returns query and preferred datasource type
	query, dsType := expandShortcut(query)

	// Auto-detect datasource ID if not specified
	if dsID == 0 {
		datasources, err := client.FetchDatasources()
		shared.Fatal(err, "fetch datasources")
		dsID = pickDatasource(datasources, dsType)
		if dsID == 0 {
			fmt.Fprintln(os.Stderr, "❌ 未找到可用数据源，使用 -d <id> 指定")
			os.Exit(1)
		}
	}

	result, err := client.PrometheusQuery(dsID, query)
	shared.Fatal(err, "prometheus query")

	if jsonMode {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	printPromResult(result)
}

// pickDatasource selects the best datasource ID based on type hint.
// ARMS Grafana has:
//   - "detail" datasources: APM metrics (requests, errors, slow queries)
//   - "apm-metrics" datasources (no "detail"): system/JVM metrics (CPU, memory, GC)
func pickDatasource(datasources []grafana.Datasource, dsType string) int {
	for _, ds := range datasources {
		switch dsType {
		case "system":
			// Prefer non-detail apm-metrics for system/JVM metrics
			if strings.Contains(ds.Name, "apm-metrics") && !strings.Contains(ds.Name, "detail") && !strings.Contains(ds.Name, "custom") {
				return ds.ID
			}
		default:
			// Prefer detail datasource for APM metrics
			if strings.Contains(ds.Name, "apm-metrics-detail") {
				return ds.ID
			}
		}
	}
	// Fallback: first non-tag datasource
	for _, ds := range datasources {
		if !strings.Contains(ds.Name, "arms_metrics") && !strings.Contains(ds.Name, "prom-arms") {
			return ds.ID
		}
	}
	if len(datasources) > 0 {
		return datasources[0].ID
	}
	return 0
}

// expandShortcut expands shorthand query names into full PromQL.
// Returns the expanded query and the preferred datasource type ("apm" or "system").
func expandShortcut(query string) (string, string) {
	parts := strings.Fields(query)
	if len(parts) == 0 {
		return query, "apm"
	}

	svc := ""
	if len(parts) > 1 {
		svc = parts[1]
	}

	switch parts[0] {
	case "services":
		return `count by (service) (arms_app_requests_count_raw)`, "apm"
	case "errors":
		if svc != "" {
			return fmt.Sprintf(`sum by (rpc,callType) (increase(arms_app_requests_error_count_raw{service="%s"}[5m])) > 0`, svc), "apm"
		}
		return `sum by (service) (increase(arms_app_requests_error_count_raw[5m])) > 0`, "apm"
	case "latency":
		if svc != "" {
			return fmt.Sprintf(`sum by (rpc,callType) (arms_app_requests_seconds_raw{service="%s"}) / sum by (rpc,callType) (arms_app_requests_count_raw{service="%s"}) > 0`, svc, svc), "apm"
		}
		return `sum by (service) (arms_app_requests_seconds_raw) / sum by (service) (arms_app_requests_count_raw) > 0`, "apm"
	case "slow-sql":
		if svc != "" {
			return fmt.Sprintf(`sum by (rpc) (increase(arms_db_requests_slow_count_raw{service="%s"}[5m])) > 0`, svc), "apm"
		}
		return `sum by (service) (increase(arms_db_requests_slow_count_raw[5m])) > 0`, "apm"
	case "cpu":
		if svc != "" {
			return fmt.Sprintf(`avg by (service,host) (100 - arms_system_cpu_idle{service="%s"})`, svc), "system"
		}
		return `avg by (service,host) (100 - arms_system_cpu_idle)`, "system"
	case "mem":
		if svc != "" {
			return fmt.Sprintf(`avg by (service,host) (arms_system_mem_used_bytes{service="%s"} / arms_system_mem_total_bytes{service="%s"} * 100)`, svc, svc), "system"
		}
		return `avg by (service,host) (arms_system_mem_used_bytes / arms_system_mem_total_bytes * 100)`, "system"
	case "gc":
		if svc != "" {
			return fmt.Sprintf(`sum by (host) (increase(arms_jvm_gc_delta{service="%s",gen="old"}[5m]))`, svc), "system"
		}
		return `sum by (service) (increase(arms_jvm_gc_delta{gen="old"}[5m]))`, "system"
	case "qps":
		if svc != "" {
			return fmt.Sprintf(`sum by (rpc,callType) (rate(arms_app_requests_count_raw{service="%s"}[1m])) > 0`, svc), "apm"
		}
		return `sum by (service) (rate(arms_app_requests_count_raw[1m])) > 0`, "apm"
	}
	return query, "apm"
}

func printPromResult(result *grafana.PromQueryResult) {
	if len(result.Data.Result) == 0 {
		fmt.Println("查询无结果")
		return
	}

	w := newTabWriter()
	// Header: metric labels + value
	fmt.Fprintf(w, "指标\t值\n")
	fmt.Fprintf(w, "━━━━\t━━\n")

	for _, sample := range result.Data.Result {
		// Build label string
		labels := formatMetricLabels(sample.Metric)
		value := ""
		if len(sample.Value) > 1 {
			value = fmt.Sprintf("%v", sample.Value[1])
		}
		fmt.Fprintf(w, "%s\t%s\n", labels, value)
	}
	w.Flush()
	fmt.Fprintf(os.Stderr, "\n共 %d 条结果\n", len(result.Data.Result))
}

func formatMetricLabels(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	// Only show important labels, skip noisy Kubernetes/ARMS internal labels
	var parts []string
	priority := []string{"service", "host", "rpc", "callType", "instance", "status", "gen", "state", "area", "id"}
	for _, k := range priority {
		if v, ok := m[k]; ok {
			parts = append(parts, k+"="+v)
		}
	}
	if len(parts) == 0 {
		// Fallback: show all labels if none matched priority
		for k, v := range m {
			if k == "__name__" {
				continue
			}
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, ", ")
}

func printQueryHelp() {
	fmt.Println(`用法: tssh arms query <promql|shortcut> [-d <数据源ID>] [-j]

快捷查询:
  services                列出所有服务
  errors [service]        错误数 (近 5 分钟)
  latency [service]       平均响应时间
  slow-sql [service]      慢 SQL 数量
  qps [service]           每秒请求数
  cpu [service]           CPU 使用率
  mem [service]           内存使用率
  gc [service]            Full GC 次数

示例:
  tssh arms query services
  tssh arms query errors backend-openapi-turingapi
  tssh arms query 'arms_app_requests_count{service="my-svc"}'`)
}
