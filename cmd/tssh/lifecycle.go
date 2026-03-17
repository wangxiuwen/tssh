package main

import (
	"fmt"
	"os"
	"time"
)

// cmdLifecycle handles stop/start/reboot instance commands
func cmdLifecycle(action string, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "用法: tssh %s <name>\n", action)
		os.Exit(1)
	}

	cache := getCache()
	ensureCache(cache)
	inst := resolveInstanceOrExit(cache, args[0])

	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	actionNames := map[string]string{
		"stop":   "关机",
		"start":  "开机",
		"reboot": "重启",
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
