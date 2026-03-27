package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
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

func getNonFlagArg(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}
	return ""
}

// Stubs for subcommands (implemented in subsequent issues)

func cmdArmsDash(args []string) {
	fmt.Fprintln(os.Stderr, "TODO: tssh arms dash")
}

func cmdArmsDs(args []string) {
	fmt.Fprintln(os.Stderr, "TODO: tssh arms ds")
}

func cmdArmsOpen(args []string) {
	fmt.Fprintln(os.Stderr, "TODO: tssh arms open")
}

func cmdArmsQuery(args []string) {
	fmt.Fprintln(os.Stderr, "TODO: tssh arms query")
}
