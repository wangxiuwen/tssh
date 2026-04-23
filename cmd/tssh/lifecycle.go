package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

// cmdLifecycle handles stop/start/reboot instance commands
func cmdLifecycle(action string, args []string) {
	assumeYes := false
	var positional []string
	for _, a := range args {
		switch a {
		case "-y", "--yes":
			assumeYes = true
		default:
			positional = append(positional, a)
		}
	}
	if len(positional) == 0 {
		fmt.Fprintf(os.Stderr, "用法: tssh %s [-y] <name>\n", action)
		os.Exit(1)
	}

	cache := getCache()
	ensureCache(cache)
	inst := resolveInstanceOrExit(cache, positional[0])

	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	actionNames := map[string]string{
		"stop":   "关机",
		"start":  "开机",
		"reboot": "重启",
	}

	// Destructive ops require explicit consent. `start` is non-destructive so
	// we never prompt. For stop/reboot:
	//   - TTY: interactive prompt (unless -y)
	//   - Non-TTY (pipeline/script): must pass -y, otherwise refuse — we don't
	//     want an accidental `echo foo | tssh stop ...` to silently proceed.
	if action == "stop" || action == "reboot" {
		if !assumeYes {
			if !isTerminal() {
				fmt.Fprintf(os.Stderr, "❌ 非交互环境 (stdin 非 TTY) 必须显式加 -y/--yes 才能%s\n", actionNames[action])
				os.Exit(2)
			}
			fmt.Fprintf(os.Stderr, "⚠️  即将%s实例 %s (%s) — 当前状态 %s\n", actionNames[action], inst.Name, inst.ID, inst.Status)
			fmt.Fprint(os.Stderr, "继续? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			ans, _ := reader.ReadString('\n')
			ans = strings.TrimSpace(strings.ToLower(ans))
			if ans != "y" && ans != "yes" {
				fmt.Fprintln(os.Stderr, "已取消")
				os.Exit(1)
			}
		}
	}

	fmt.Printf("⚡ %s %s (%s)...\n", actionNames[action], inst.Name, inst.ID)

	switch action {
	case "stop":
		err = client.StopInstance(inst.ID)
	case "start":
		err = client.StartInstance(inst.ID)
	case "reboot":
		err = client.RebootInstance(inst.ID)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %s 失败: %v\n", actionNames[action], err)
		os.Exit(1)
	}

	fmt.Printf("✅ %s 指令已发送\n", actionNames[action])

	// Poll for status change
	fmt.Print("   等待状态变更...")
	expectedStatus := map[string]string{
		"stop":   "Stopped",
		"start":  "Running",
		"reboot": "Running",
	}

	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)
		detail, err := client.GetInstanceDetail(inst.ID)
		if err != nil {
			continue
		}
		// Check status from the cached instance list refresh
		_ = detail
		fmt.Print(".")

		// Use DescribeInstances to get status
		instances, _ := client.FetchInstanceByID(inst.ID)
		if len(instances) > 0 && instances[0].Status == expectedStatus[action] {
			fmt.Printf("\n✅ %s 当前状态: %s\n", inst.Name, instances[0].Status)
			return
		}
	}
	fmt.Printf("\n⚠️  操作已发送，请稍后检查状态\n")
}
