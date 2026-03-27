package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/wangxiuwen/tssh/internal/grafana"
)

// cmdArms routes arms subcommands
func cmdArms(args []string) {
	if len(args) == 0 {
		// Default: show alerts
		cmdArmsAlerts(nil)
		return
	}
	switch args[0] {
	case "alerts":
		cmdArmsAlerts(args[1:])
	case "dash":
		cmdArmsDash(args[1:])
	case "ds":
		cmdArmsDs(args[1:])
	case "open":
		cmdArmsOpen(args[1:])
	case "query":
		cmdArmsQuery(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n用法: tssh arms [alerts|dash|ds|open|query]\n", args[0])
		os.Exit(1)
	}
}

func mustGrafanaClient() *GrafanaClient {
	cfg, err := LoadGrafanaConfig()
	fatal(err, "load grafana config")
	return NewGrafanaClient(cfg)
}

// cmdArmsAlerts shows currently firing alerts
func cmdArmsAlerts(args []string) {
	jsonMode := hasFlag(args, "-j", "--json")

	client := mustGrafanaClient()
	alerts, err := client.FetchAlerts()
	fatal(err, "fetch alerts")

	if jsonMode {
		data, _ := json.MarshalIndent(alerts, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(alerts) == 0 {
		fmt.Println("✅ 当前无触发中的告警")
		return
	}

	// Sort by severity: critical > warning > info > others
	sort.Slice(alerts, func(i, j int) bool {
		return severityOrder(alerts[i].Labels["severity"]) < severityOrder(alerts[j].Labels["severity"])
	})

	fmt.Printf("🔥 当前告警 (%d 个触发中)\n", len(alerts))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	for _, alert := range alerts {
		printAlert(alert)
	}
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
		fmt.Printf("   持续: %s\n", formatDuration(dur))
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
	fatal(err, "search dashboards")

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
	fatal(err, "fetch datasources")

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
	fatal(err, "search dashboards")

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
	exec := execCommand(cmd, url)
	exec.Start()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func cmdArmsQuery(args []string) {
	fmt.Fprintln(os.Stderr, "TODO: tssh arms query")
}
