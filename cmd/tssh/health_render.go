package main

import (
	"fmt"
	"strings"
	"time"
)

func shortenName(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}
	// Try to keep the last segment (usually most meaningful)
	parts := strings.Split(name, "-")
	if len(parts) > 3 {
		// Keep first 2 and last part
		short := parts[0] + "-" + parts[1] + "-.." + parts[len(parts)-1]
		if len(short) <= maxLen {
			return short
		}
	}
	return name[:maxLen-2] + ".."
}

func renderHealthTable(results []healthResult, running, totalAlerts int, alertOnly bool) string {
	var sb strings.Builder
	tw := getTermWidth()

	if alertOnly {
		if totalAlerts == 0 {
			sb.WriteString("✅ 所有机器状态正常! 无告警\n")
			return sb.String()
		}
		nameW := 25
		if tw < 75 {
			nameW = tw - 30
			if nameW < 12 {
				nameW = 12
			}
		}
		sb.WriteString(fmt.Sprintf("🚨 发现 %d 条告警:\n\n", totalAlerts))
		for _, r := range results {
			if r.Skipped || len(r.Alerts) == 0 {
				continue
			}
			sb.WriteString(fmt.Sprintf("  %-*s  %s\n", nameW, shortenName(r.Name, nameW), r.IP))
			for _, a := range r.Alerts {
				sb.WriteString(fmt.Sprintf("    ⚠ %s\n", a))
			}
		}
		return sb.String()
	}

	if tw >= 95 {
		// Wide: all columns
		nameW := 25
		sep := strings.Repeat("─", tw-2)
		if len(sep) > 95 {
			sep = strings.Repeat("─", 95)
		}
		sb.WriteString(fmt.Sprintf("%-2s %-*s  %4s %5s  %6s %4s %5s  %s\n", "ST", nameW, "NAME", "CPU", "LOAD", "MEM", "MEM%", "DISK%", "NET"))
		sb.WriteString(sep + "\n")

		for _, r := range results {
			if r.Skipped {
				continue
			}
			if r.Error != "" {
				sb.WriteString(fmt.Sprintf("❌ %-*s  %s\n", nameW, shortenName(r.Name, nameW), r.Error))
				continue
			}
			icon := "✅"
			if len(r.Alerts) > 0 {
				icon = "🚨"
			}
			memStr := fmt.Sprintf("%dG/%dG", r.MemUsedMB/1024, r.MemTotalMB/1024)
			netStr := fmt.Sprintf("%d/%d", r.NetEstab, r.NetTW)

			sb.WriteString(fmt.Sprintf("%s %-*s  %3dc %5.1f  %6s %3d%% %4d%%  %s",
				icon, nameW, shortenName(r.Name, nameW),
				r.CPUCores, r.CPULoad, memStr, r.MemPct, r.DiskPct, netStr))
			if len(r.JVMs) > 0 {
				sb.WriteString(fmt.Sprintf("  ☕%d", len(r.JVMs)))
			}
			sb.WriteString("\n")
			for _, a := range r.Alerts {
				sb.WriteString(fmt.Sprintf("   ⚠ %s\n", a))
			}
		}
	} else if tw >= 70 {
		// Medium: drop NET column
		nameW := tw - 45
		if nameW < 15 {
			nameW = 15
		}
		sep := strings.Repeat("─", tw-2)
		sb.WriteString(fmt.Sprintf("%-2s %-*s %4s %5s %4s %5s\n", "ST", nameW, "NAME", "CPU", "LOAD", "MEM%", "DISK%"))
		sb.WriteString(sep + "\n")

		for _, r := range results {
			if r.Skipped {
				continue
			}
			if r.Error != "" {
				sb.WriteString(fmt.Sprintf("❌ %-*s  %s\n", nameW, shortenName(r.Name, nameW), r.Error))
				continue
			}
			icon := "✅"
			if len(r.Alerts) > 0 {
				icon = "🚨"
			}
			sb.WriteString(fmt.Sprintf("%s %-*s %3dc %5.1f %3d%% %4d%%",
				icon, nameW, shortenName(r.Name, nameW),
				r.CPUCores, r.CPULoad, r.MemPct, r.DiskPct))
			if len(r.JVMs) > 0 {
				sb.WriteString(fmt.Sprintf(" ☕%d", len(r.JVMs)))
			}
			sb.WriteString("\n")
			for _, a := range r.Alerts {
				sb.WriteString(fmt.Sprintf("  ⚠ %s\n", a))
			}
		}
	} else {
		// Narrow: compact
		nameW := tw - 22
		if nameW < 10 {
			nameW = 10
		}
		sep := strings.Repeat("─", tw-2)
		sb.WriteString(fmt.Sprintf("%-2s %-*s %4s %4s\n", "ST", nameW, "NAME", "MEM%", "DSK%"))
		sb.WriteString(sep + "\n")

		for _, r := range results {
			if r.Skipped {
				continue
			}
			if r.Error != "" {
				sb.WriteString(fmt.Sprintf("❌ %-*s err\n", nameW, shortenName(r.Name, nameW)))
				continue
			}
			icon := "✅"
			if len(r.Alerts) > 0 {
				icon = "🚨"
			}
			sb.WriteString(fmt.Sprintf("%s %-*s %3d%% %3d%%\n",
				icon, nameW, shortenName(r.Name, nameW), r.MemPct, r.DiskPct))
			for _, a := range r.Alerts {
				sb.WriteString(fmt.Sprintf("  ⚠ %s\n", a))
			}
		}
	}

	sb.WriteString(fmt.Sprintf("\n📊 共 %d 台, %d 条告警\n", running, totalAlerts))

	// Problem summary table at the end — grouped by machine
	if totalAlerts > 0 {
		// Separate into critical (OOM, CPU, MEM, DISK) and warning (TIME_WAIT, zombie)
		var critical, warning []string
		for _, r := range results {
			if r.Skipped || len(r.Alerts) == 0 {
				continue
			}
			nameW := 22
			if tw < 75 {
				nameW = tw - 35
				if nameW < 12 {
					nameW = 12
				}
			}
			name := shortenName(r.Name, nameW)
			alertStr := strings.Join(r.Alerts, ", ")

			hasCritical := false
			for _, a := range r.Alerts {
				if strings.Contains(a, "OOM") || strings.Contains(a, "CPU") || strings.Contains(a, "内存") || strings.Contains(a, "磁盘") || strings.Contains(a, "JVM") {
					hasCritical = true
					break
				}
			}
			line := fmt.Sprintf("  %-*s  %s", nameW, name, alertStr)
			if hasCritical {
				critical = append(critical, line)
			} else {
				warning = append(warning, line)
			}
		}

		sb.WriteString("\n╔══ 问题汇总 ══════════════════════════════════════\n")
		if len(critical) > 0 {
			sb.WriteString(fmt.Sprintf("║ 🔥 严重 (%d)\n", len(critical)))
			for _, l := range critical {
				sb.WriteString(fmt.Sprintf("║%s\n", l))
			}
		}
		if len(warning) > 0 {
			if len(critical) > 0 {
				sb.WriteString("║─────────────────────────────────────────────────\n")
			}
			sb.WriteString(fmt.Sprintf("║ ⚠  警告 (%d)\n", len(warning)))
			for _, l := range warning {
				sb.WriteString(fmt.Sprintf("║%s\n", l))
			}
		}
		sb.WriteString("╚═════════════════════════════════════════════════\n")
	}

	return sb.String()
}

func renderHealthMarkdown(results []healthResult, running, totalAlerts int, alertOnly bool) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# 服务器巡检报告\n\n"))
	sb.WriteString(fmt.Sprintf("- **时间**: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("- **巡检台数**: %d\n", running))
	sb.WriteString(fmt.Sprintf("- **告警数量**: %d\n\n", totalAlerts))

	// Alerts summary
	if totalAlerts > 0 {
		sb.WriteString("## ⚠️ 告警汇总\n\n")
		sb.WriteString("| 机器 | IP | 告警 |\n")
		sb.WriteString("|------|----|------|\n")
		for _, r := range results {
			if r.Skipped || len(r.Alerts) == 0 {
				continue
			}
			for _, a := range r.Alerts {
				sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", r.Name, r.IP, a))
			}
		}
		sb.WriteString("\n")
	}

	if !alertOnly {
		// Full details table
		sb.WriteString("## 详细数据\n\n")
		sb.WriteString("| 状态 | 机器名 | CPU | Load | 内存 | 内存% | 磁盘% | Estab | TW | JVM |\n")
		sb.WriteString("|------|--------|-----|------|------|-------|-------|-------|-----|-----|\n")
		for _, r := range results {
			if r.Skipped || r.Error != "" {
				continue
			}
			icon := "✅"
			if len(r.Alerts) > 0 {
				icon = "🚨"
			}
			jvmStr := "-"
			if len(r.JVMs) > 0 {
				jvmStr = fmt.Sprintf("%d个", len(r.JVMs))
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | %dc | %.1f | %dG/%dG | %d%% | %d%% | %d | %d | %s |\n",
				icon, r.Name, r.CPUCores, r.CPULoad,
				r.MemUsedMB/1024, r.MemTotalMB/1024, r.MemPct,
				r.DiskPct, r.NetEstab, r.NetTW, jvmStr))
		}
	}

	return sb.String()
}

func renderHealthCSV(results []healthResult) string {
	var sb strings.Builder
	sb.WriteString("name,ip,cpu_cores,cpu_load,mem_used_mb,mem_total_mb,mem_pct,disk_pct,net_estab,net_tw,jvm_count,alerts\n")
	for _, r := range results {
		if r.Skipped {
			continue
		}
		alerts := strings.Join(r.Alerts, "; ")
		sb.WriteString(fmt.Sprintf("%s,%s,%d,%.1f,%d,%d,%d,%d,%d,%d,%d,\"%s\"\n",
			r.Name, r.IP, r.CPUCores, r.CPULoad,
			r.MemUsedMB, r.MemTotalMB, r.MemPct, r.DiskPct,
			r.NetEstab, r.NetTW, len(r.JVMs), alerts))
	}
	return sb.String()
}
