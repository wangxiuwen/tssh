package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
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
