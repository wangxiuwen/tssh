package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// cmdTail follows a remote log file by polling tail -n via RunCommand
func cmdTail(args []string) {
	interval := 2 * time.Second
	lines := 20
	var target, path string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--interval", "-i":
			if i+1 < len(args) {
				i++
				sec, err := parseTimeoutSec(args[i])
				if err != nil || sec <= 0 {
					fmt.Fprintf(os.Stderr, "❌ --interval %s: 需要正整数或时长 (如 2s / 1m)\n", args[i])
					os.Exit(2)
				}
				interval = time.Duration(sec) * time.Second
			}
		case "-n":
			if i+1 < len(args) {
				i++
				n, err := strconv.Atoi(args[i])
				if err != nil || n <= 0 {
					fmt.Fprintf(os.Stderr, "❌ -n %s: 需要正整数\n", args[i])
					os.Exit(2)
				}
				lines = n
			}
		default:
			if target == "" {
				target = args[i]
			} else {
				path = args[i]
			}
		}
	}

	if target == "" || path == "" {
		fmt.Fprintln(os.Stderr, "用法: tssh tail <name> <path> [-n lines] [--interval sec]")
		os.Exit(1)
	}

	cache := getCache()
	ensureCache(cache)
	inst := resolveInstanceOrExit(cache, target)

	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	fmt.Fprintf(os.Stderr, "📜 跟踪 %s:%s (每 %v 刷新)\n", inst.Name, path, interval)
	fmt.Fprintf(os.Stderr, "   按 Ctrl+C 退出\n\n")

	lastContent := ""
	firstRun := true

	for {
		// path is user-supplied; quote to prevent shell injection (e.g. `; rm -rf /`).
		cmd := fmt.Sprintf("tail -n %d '%s' 2>&1", lines, shellQuote(path))
		result, err := client.RunCommand(inst.ID, cmd, 10)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  %v\n", err)
			time.Sleep(interval)
			continue
		}

		content := decodeOutput(result.Output)

		if firstRun {
			fmt.Print(content)
			lastContent = content
			firstRun = false
		} else if content != lastContent {
			// Find new lines by diffing
			oldLines := strings.Split(lastContent, "\n")
			newLines := strings.Split(content, "\n")

			// Find where new content diverges
			startIdx := 0
			for i := len(newLines) - 1; i >= 0; i-- {
				found := false
				for j := len(oldLines) - 1; j >= 0; j-- {
					if newLines[i] == oldLines[j] {
						startIdx = i + 1
						found = true
						break
					}
				}
				if found {
					break
				}
			}

			if startIdx < len(newLines) {
				for _, line := range newLines[startIdx:] {
					if line != "" {
						fmt.Println(line)
					}
				}
			}
			lastContent = content
		}

		time.Sleep(interval)
	}
}
