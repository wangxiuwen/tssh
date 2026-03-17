package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// tunnelEntry represents a persistent tunnel
type tunnelEntry struct {
	ID         string `json:"id"`
	Instance   string `json:"instance"`
	InstanceID string `json:"instance_id"`
	LocalPort  int    `json:"local_port"`
	RemoteHost string `json:"remote_host"`
	RemotePort int    `json:"remote_port"`
	PID        int    `json:"pid"`
	StartTime  string `json:"start_time"`
}

// cmdTunnel manages persistent port forwarding tunnels
func cmdTunnel(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, `用法:
  tssh tunnel start <name> -L <spec>   启动后台隧道
  tssh tunnel list                     查看活跃隧道
  tssh tunnel stop <id>                关闭隧道
  tssh tunnel stop all                 关闭所有隧道`)
		os.Exit(1)
	}

	switch args[0] {
	case "start":
		tunnelStart(args[1:])
	case "list", "ls":
		tunnelList()
	case "stop":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: tssh tunnel stop <id|all>")
			os.Exit(1)
		}
		tunnelStop(args[1])
	default:
		fmt.Fprintf(os.Stderr, "未知命令: tssh tunnel %s\n", args[0])
		os.Exit(1)
	}
}

func tunnelStart(args []string) {
	var target, spec string
	for i := 0; i < len(args); i++ {
		if args[i] == "-L" && i+1 < len(args) {
			spec = args[i+1]
			i++
		} else {
			target = args[i]
		}
	}

	if target == "" || spec == "" {
		fmt.Fprintln(os.Stderr, "用法: tssh tunnel start <name> -L <port> 或 -L <local>:<host>:<remote>")
		os.Exit(1)
	}

	// Parse spec with sugar
	parts := strings.SplitN(spec, ":", 3)
	switch len(parts) {
	case 1:
		parts = []string{parts[0], "localhost", parts[0]}
	case 2:
		parts = []string{parts[0], parts[1], parts[0]}
	}

	localPort, _ := strconv.Atoi(parts[0])
	remoteHost := parts[1]
	remotePort, _ := strconv.Atoi(parts[2])

	cache := getCache()
	ensureCache(cache)
	inst := resolveInstanceOrExit(cache, target)
	config := mustLoadConfig()

	fmt.Printf("🔗 启动隧道: 127.0.0.1:%d → %s:%d (via %s)\n", localPort, remoteHost, remotePort, inst.Name)

	// Start portforward in background
	cmdArgs := []string{
		"ali-instance-cli", "portforward",
		"--instance", inst.ID,
		"--local-port", strconv.Itoa(localPort),
		"--remote-port", strconv.Itoa(remotePort),
		"--region", config.Region,
		"--access-key-id", config.AccessKeyID,
		"--access-key-secret", config.AccessKeySecret,
	}

	cmd := execCommand(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ 启动失败: %v\n", err)
		os.Exit(1)
	}

	// Save tunnel entry
	entry := tunnelEntry{
		ID:         fmt.Sprintf("t%d", time.Now().Unix()%10000),
		Instance:   inst.Name,
		InstanceID: inst.ID,
		LocalPort:  localPort,
		RemoteHost: remoteHost,
		RemotePort: remotePort,
		PID:        cmd.Process.Pid,
		StartTime:  time.Now().Format("15:04:05"),
	}

	entries := loadTunnels()
	entries = append(entries, entry)
	saveTunnels(entries)

	fmt.Printf("✅ 隧道已启动 [%s] PID=%d\n", entry.ID, entry.PID)
}

func tunnelList() {
	entries := loadTunnels()
	alive := cleanDeadTunnels(entries)
	saveTunnels(alive)

	if len(alive) == 0 {
		fmt.Println("没有活跃的隧道")
		return
	}

	fmt.Printf("%-8s %-20s %8s %-22s %6s %s\n", "ID", "Instance", "Local", "Remote", "PID", "Start")
	fmt.Println(strings.Repeat("─", 80))
	for _, e := range alive {
		remote := fmt.Sprintf("%s:%d", e.RemoteHost, e.RemotePort)
		fmt.Printf("%-8s %-20s :%d → %-20s %6d %s\n", e.ID, e.Instance, e.LocalPort, remote, e.PID, e.StartTime)
	}
}

func tunnelStop(id string) {
	entries := loadTunnels()

	if id == "all" {
		for _, e := range entries {
			killProcess(e.PID)
			fmt.Printf("🛑 已关闭 [%s] %s:%d (PID %d)\n", e.ID, e.Instance, e.LocalPort, e.PID)
		}
		saveTunnels(nil)
		return
	}

	var remaining []tunnelEntry
	found := false
	for _, e := range entries {
		if e.ID == id {
			killProcess(e.PID)
			fmt.Printf("🛑 已关闭 [%s] %s:%d (PID %d)\n", e.ID, e.Instance, e.LocalPort, e.PID)
			found = true
		} else {
			remaining = append(remaining, e)
		}
	}

	if !found {
		fmt.Fprintf(os.Stderr, "❌ 未找到隧道 %s\n", id)
		os.Exit(1)
	}

	saveTunnels(remaining)
}

func tunnelsFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tssh", "tunnels.json")
}

func loadTunnels() []tunnelEntry {
	data, err := os.ReadFile(tunnelsFile())
	if err != nil {
		return nil
	}
	var entries []tunnelEntry
	json.Unmarshal(data, &entries)
	return entries
}

func saveTunnels(entries []tunnelEntry) {
	os.MkdirAll(filepath.Dir(tunnelsFile()), 0755)
	data, _ := json.Marshal(entries)
	os.WriteFile(tunnelsFile(), data, 0644)
}

func cleanDeadTunnels(entries []tunnelEntry) []tunnelEntry {
	var alive []tunnelEntry
	for _, e := range entries {
		if processAlive(e.PID) {
			alive = append(alive, e)
		}
	}
	return alive
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks if process exists
	return p.Signal(nil) == nil
}

func killProcess(pid int) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	p.Kill()
}
