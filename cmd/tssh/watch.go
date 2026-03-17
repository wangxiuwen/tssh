package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// cmdWatch periodically executes a command and refreshes output
func cmdWatch(args []string) {
	interval := 5 * time.Second
	pattern := ""
	var targets []string
	var command []string

	i := 0
	for i < len(args) {
		switch args[i] {
		case "-g", "--grep":
			if i+1 < len(args) {
				pattern = args[i+1]
				i += 2
			}
		case "--interval", "-n":
			if i+1 < len(args) {
				sec, err := strconv.Atoi(args[i+1])
				if err == nil {
					interval = time.Duration(sec) * time.Second
				}
				i += 2
			}
		default:
			if len(command) == 0 && pattern == "" && len(targets) == 0 {
				targets = append(targets, args[i])
				i++
			} else {
				command = args[i:]
				i = len(args)
			}
		}
	}

	if len(command) == 0 {
		fmt.Fprintln(os.Stderr, "用法: tssh watch [-g <pat>] [--interval <sec>] <target> <cmd>")
		os.Exit(1)
	}

	cmdStr := strings.Join(command, " ")

	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	cache := getCache()
	ensureCache(cache)

	var instances []Instance
	if pattern != "" {
		instances, _ = cache.FindByPattern(pattern)
	} else if len(targets) > 0 {
		inst := resolveInstanceOrExit(cache, targets[0])
		instances = append(instances, *inst)
	}

	if len(instances) == 0 {
		fmt.Fprintln(os.Stderr, "❌ 没有匹配的实例")
		os.Exit(1)
	}

	// Handle Ctrl+C gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	iteration := 0
	for {
		iteration++

		// Clear screen
		fmt.Print("\033[2J\033[H")
		fmt.Printf("🔄 tssh watch — 每 %v 刷新 (第 %d 次) | %s\n", interval, iteration, time.Now().Format("15:04:05"))
		fmt.Printf("   命令: %s | 目标: %d 台\n", cmdStr, len(instances))
		fmt.Println(strings.Repeat("─", 60))

		for _, inst := range instances {
			if inst.Status != "Running" {
				fmt.Printf("\n[%s] ⏹ not running\n", inst.Name)
				continue
			}
			result, err := client.RunCommand(inst.ID, cmdStr, 10)
			if err != nil {
				fmt.Printf("\n[%s] ❌ %v\n", inst.Name, err)
				continue
			}
			output := strings.TrimRight(decodeOutput(result.Output), "\n")
			if len(instances) > 1 {
				fmt.Printf("\n[%s]\n", inst.Name)
			}
			fmt.Println(output)
		}

		select {
		case <-sigCh:
			fmt.Println("\n👋 已停止")
			return
		case <-time.After(interval):
		}
	}
}
