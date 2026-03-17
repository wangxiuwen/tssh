package main

import (
"encoding/json"
"fmt"
"os"
"strconv"
"strings"
"sync"
"time"
)

// cmdHealth runs health checks / smart inspection on instances
func cmdHealth(args []string) {
	pattern := ""
	alertOnly := false
	outputFormat := "table" // table, json, md, csv
	outputFile := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-g", "--grep":
			if i+1 < len(args) {
				pattern = args[i+1]
				i++
			}
		case "-j", "--json":
			outputFormat = "json"
		case "--alert", "-a":
			alertOnly = true
		case "--format":
			if i+1 < len(args) {
				outputFormat = args[i+1]
				i++
			}
		case "-o", "--output":
			if i+1 < len(args) {
				outputFile = args[i+1]
				i++
			}
		default:
			if pattern == "" {
				pattern = args[i]
			}
		}
	}

	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	cache := getCache()
	ensureCache(cache)

	var targets []Instance
	if pattern != "" {
		targets, _ = cache.FindByPattern(pattern)
	} else {
		all, _ := cache.Load()
		targets = all
	}

	running := 0
	for _, t := range targets {
		if t.Status == "Running" {
			running++
		}
	}

	if running == 0 {
		fmt.Fprintln(os.Stderr, "❌ 没有运行中的实例")
		os.Exit(1)
	}

	// Deep inspection script
	healthScript := `#!/bin/bash
cores=$(grep -c ^processor /proc/cpuinfo)
load1=$(cat /proc/loadavg | cut -d' ' -f1)
echo "cpu_cores:$cores"
echo "cpu_load:$load1"
mem_info=$(free -m | awk '/^Mem:/{printf "%d %d %.0f", $3, $2, $3/$2*100}')
echo "mem:$mem_info"
disk_info=$(df / | awk 'NR==2{printf "%d %d %.0f", $3, $2, $3/$2*100}')
echo "disk:$disk_info"
echo "uptime:$(uptime -p 2>/dev/null || uptime | sed 's/.*up /up /' | sed 's/,.*//')"
zombies=$(ps aux 2>/dev/null | awk '$8 ~ /Z/{count++} END{print count+0}')
echo "zombies:$zombies"
oom=$(dmesg -T 2>/dev/null | grep -c "Out of memory" || echo 0)
echo "oom:$oom"
jvm_pids=$(pgrep -f 'java ' 2>/dev/null | head -5)
if [ -n "$jvm_pids" ]; then
  for pid in $jvm_pids; do
    cmdline=$(cat /proc/$pid/cmdline 2>/dev/null | tr '\0' ' ' | sed 's/.*\///' | cut -c1-30)
    heap=$(jstat -gc $pid 2>/dev/null | awk 'NR==2{u=$3+$4+$6+$8;m=$1+$2+$5+$7;if(m>0)printf "%.0f",u/m*100;else print "0"}')
    if [ -z "$heap" ]; then
      rss=$(ps -o rss= -p $pid 2>/dev/null | tr -d ' ')
      echo "jvm:$pid|$cmdline|rss=${rss}KB"
    else
      echo "jvm:$pid|$cmdline|heap=${heap}%"
    fi
  done
fi
tw=$(ss -s 2>/dev/null | awk '/timewait/{gsub(/[^0-9]/,"",$2); print $2}')
estab=$(ss -s 2>/dev/null | awk '/estab/{gsub(/[^0-9]/,"",$2); print $2}')
echo "net_tw:${tw:-0}"
echo "net_estab:${estab:-0}"
`

	fmt.Fprintf(os.Stderr, "🏥 深度巡检 %d 台机器...\n\n", running)

	results := make([]healthResult, len(targets))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for i, inst := range targets {
		if inst.Status != "Running" {
			results[i] = healthResult{Name: inst.Name, Skipped: true}
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, inst Instance) {
			defer wg.Done()
			defer func() { <-sem }()
			result, err := client.RunCommand(inst.ID, healthScript, 20)
			r := healthResult{Name: inst.Name, IP: inst.PrivateIP}
			if err != nil {
				r.Error = err.Error()
				r.Alerts = append(r.Alerts, "巡检失败")
			} else {
				output := decodeOutput(result.Output)
				for _, line := range strings.Split(output, "\n") {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "cpu_cores:") {
						r.CPUCores, _ = strconv.Atoi(line[10:])
					} else if strings.HasPrefix(line, "cpu_load:") {
						fmt.Sscanf(line[9:], "%f", &r.CPULoad)
					} else if strings.HasPrefix(line, "mem:") {
						fmt.Sscanf(line[4:], "%d %d %d", &r.MemUsedMB, &r.MemTotalMB, &r.MemPct)
					} else if strings.HasPrefix(line, "disk:") {
						parts := strings.Fields(line[5:])
						if len(parts) >= 3 {
							r.DiskPct, _ = strconv.Atoi(parts[2])
						}
					} else if strings.HasPrefix(line, "uptime:") {
						r.Uptime = line[7:]
					} else if strings.HasPrefix(line, "zombies:") {
						r.Zombies, _ = strconv.Atoi(line[8:])
					} else if strings.HasPrefix(line, "oom:") {
						r.OOMKills, _ = strconv.Atoi(line[4:])
					} else if strings.HasPrefix(line, "jvm:") {
						parts := strings.SplitN(line[4:], "|", 3)
						if len(parts) >= 3 {
							r.JVMs = append(r.JVMs, jvmInfo{PID: parts[0], Name: parts[1], Heap: parts[2]})
						}
					} else if strings.HasPrefix(line, "net_tw:") {
						r.NetTW, _ = strconv.Atoi(line[7:])
					} else if strings.HasPrefix(line, "net_estab:") {
						r.NetEstab, _ = strconv.Atoi(line[10:])
					}
				}

				// Threshold-based anomaly detection
				if r.CPUCores > 0 && r.CPULoad > float64(r.CPUCores)*0.8 {
					r.Alerts = append(r.Alerts, fmt.Sprintf("CPU过载 load=%.1f/%d核", r.CPULoad, r.CPUCores))
				}
				if r.MemPct > 85 {
					r.Alerts = append(r.Alerts, fmt.Sprintf("内存紧张 %d%%", r.MemPct))
				}
				if r.DiskPct > 85 {
					r.Alerts = append(r.Alerts, fmt.Sprintf("磁盘告警 %d%%", r.DiskPct))
				}
				if r.Zombies > 0 {
					r.Alerts = append(r.Alerts, fmt.Sprintf("僵尸进程 %d个", r.Zombies))
				}
				if r.OOMKills > 0 {
					r.Alerts = append(r.Alerts, fmt.Sprintf("OOM Kill %d次", r.OOMKills))
				}
				if r.NetTW > 5000 {
					r.Alerts = append(r.Alerts, fmt.Sprintf("TIME_WAIT %d", r.NetTW))
				}
				for _, jvm := range r.JVMs {
					if strings.Contains(jvm.Heap, "heap=") {
						heapStr := strings.TrimPrefix(jvm.Heap, "heap=")
						heapStr = strings.TrimSuffix(heapStr, "%")
						if v, e := strconv.Atoi(heapStr); e == nil && v > 85 {
							r.Alerts = append(r.Alerts, fmt.Sprintf("JVM堆 %s=%d%%", jvm.Name, v))
						}
					}
				}
			}
			results[idx] = r
		}(i, inst)
	}
	wg.Wait()

	// Count alerts
	totalAlerts := 0
	for _, r := range results {
		totalAlerts += len(r.Alerts)
	}

	// Render output
	var output string
	switch outputFormat {
	case "json":
		data, _ := json.MarshalIndent(results, "", "  ")
		output = string(data)
	case "md", "markdown":
		output = renderHealthMarkdown(results, running, totalAlerts, alertOnly)
	case "csv":
		output = renderHealthCSV(results)
	default:
		output = renderHealthTable(results, running, totalAlerts, alertOnly)
	}

	if outputFile != "" {
		err := os.WriteFile(outputFile, []byte(output), 0644)
		fatal(err, "write output")
		fmt.Fprintf(os.Stderr, "✅ 报告已保存到 %s\n", outputFile)
	} else {
		fmt.Print(output)
	}
}


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

// healthResult type must be file-level for render functions
type healthResult struct {
	Name       string    `json:"name"`
	IP         string    `json:"ip"`
	CPUCores   int       `json:"cpu_cores"`
	CPULoad    float64   `json:"cpu_load"`
	MemUsedMB  int       `json:"mem_used_mb"`
	MemTotalMB int       `json:"mem_total_mb"`
	MemPct     int       `json:"mem_pct"`
	DiskPct    int       `json:"disk_pct"`
	Zombies    int       `json:"zombies"`
	OOMKills   int       `json:"oom_kills"`
	NetTW      int       `json:"net_timewait"`
	NetEstab   int       `json:"net_established"`
	JVMs       []jvmInfo `json:"jvms,omitempty"`
	Uptime     string    `json:"uptime,omitempty"`
	Alerts     []string  `json:"alerts,omitempty"`
	Error      string    `json:"error,omitempty"`
	Skipped    bool      `json:"skipped,omitempty"`
}

type jvmInfo struct {
	PID  string `json:"pid"`
	Name string `json:"name"`
	Heap string `json:"heap"`
}

