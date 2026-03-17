package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// cmdTop shows a live dashboard of instance status (like htop)
func cmdTop(args []string) {
	interval := 10 * time.Second
	pattern := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-g", "--grep":
			if i+1 < len(args) {
				pattern = args[i+1]
				i++
			}
		case "--interval", "-n":
			if i+1 < len(args) {
				sec := 0
				fmt.Sscanf(args[i+1], "%d", &sec)
				if sec > 0 {
					interval = time.Duration(sec) * time.Second
				}
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

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	topScript := `cores=$(grep -c ^processor /proc/cpuinfo)
load1=$(cat /proc/loadavg | cut -d' ' -f1)
mem=$(free -m | awk '/^Mem:/{printf "%d/%dM %d%%", $3, $2, $3/$2*100}')
disk=$(df / | awk 'NR==2{printf "%d%%", $5}')
echo "$cores|$load1|$mem|$disk"`

	iteration := 0
	for {
		iteration++

		type topResult struct {
			Name    string
			CPU     string
			Load    string
			Mem     string
			Disk    string
			Error   string
		}

		results := make([]topResult, len(targets))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 5)

		for i, inst := range targets {
			if inst.Status != "Running" {
				results[i] = topResult{Name: inst.Name, Error: "stopped"}
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, inst Instance) {
				defer wg.Done()
				defer func() { <-sem }()
				result, err := client.RunCommand(inst.ID, topScript, 10)
				r := topResult{Name: inst.Name}
				if err != nil {
					r.Error = "timeout"
				} else {
					output := strings.TrimSpace(decodeOutput(result.Output))
					parts := strings.SplitN(output, "|", 4)
					if len(parts) == 4 {
						r.CPU = parts[0] + "c"
						r.Load = parts[1]
						r.Mem = parts[2]
						r.Disk = parts[3]
					} else {
						r.Error = "parse"
					}
				}
				results[idx] = r
			}(i, inst)
		}
		wg.Wait()

		// Render
		fmt.Print("\033[2J\033[H")
		fmt.Printf("📊 tssh top — %d 台实例 | 每 %v 刷新 | %s\n", running, interval, time.Now().Format("15:04:05"))
		fmt.Println(strings.Repeat("─", 70))
		fmt.Printf("%-25s %5s %6s  %-18s %6s\n", "NAME", "CPU", "LOAD", "MEM", "DISK")
		fmt.Println(strings.Repeat("─", 70))

		for _, r := range results {
			if r.Error != "" {
				fmt.Printf("%-25s %s\n", shortenName(r.Name, 25), "⚠️ "+r.Error)
				continue
			}
			fmt.Printf("%-25s %5s %6s  %-18s %6s\n", shortenName(r.Name, 25), r.CPU, r.Load, r.Mem, r.Disk)
		}
		fmt.Printf("\n按 Ctrl+C 退出 (第 %d 次刷新)\n", iteration)

		select {
		case <-sigCh:
			fmt.Println("\n👋 已退出")
			return
		case <-time.After(interval):
		}
	}
}
