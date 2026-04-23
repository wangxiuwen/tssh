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

var tunnelGroup = CmdGroup{
	Name: "tunnel",
	Desc: "持久化端口转发隧道管理",
	Commands: []SubCmd{
		{Name: "start", Desc: "启动后台隧道 <name> -L <spec>", Run: tunnelStart},
		{Name: "list", Aliases: []string{"ls"}, Desc: "查看活跃隧道", Run: func(args []string) { tunnelList() }},
		{Name: "stop", Desc: "关闭隧道 <id|all>", Run: func(args []string) {
			if len(args) < 1 {
				fmt.Fprintln(os.Stderr, "用法: tssh tunnel stop <id|all>")
				os.Exit(1)
			}
			tunnelStop(args[0])
		}},
	},
}

func cmdTunnel(args []string) { tunnelGroup.Dispatch(args) }

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

	parts := strings.SplitN(spec, ":", 3)
	switch len(parts) {
	case 1:
		parts = []string{parts[0], "localhost", parts[0]}
	case 2:
		parts = []string{parts[0], parts[1], parts[0]}
	}

	localPort, lerr := strconv.Atoi(parts[0])
	remoteHost := parts[1]
	remotePort, rerr := strconv.Atoi(parts[2])
	if lerr != nil || rerr != nil || localPort <= 0 || localPort > 65535 || remotePort <= 0 || remotePort > 65535 {
		fmt.Fprintf(os.Stderr, "❌ 端口号无效: %s\n", spec)
		os.Exit(2)
	}

	// tunnel 走后台 daemon (_portforward), 目前只支持直连 ECS localhost.
	// 远端主机中转 (socat) 需要清理远端进程, daemon 模式下不好处理; 让用户
	// 用前台 `tssh -L <local>:<host>:<remote> <name>` 模式 (会在退出时清理 socat).
	if remoteHost != "" && remoteHost != "localhost" && remoteHost != "127.0.0.1" {
		fmt.Fprintf(os.Stderr, "❌ tunnel 后台模式暂不支持远端主机中转 (-L %d:%s:%d)\n", localPort, remoteHost, remotePort)
		fmt.Fprintln(os.Stderr, "   请用前台: tssh -L "+spec+" "+target)
		os.Exit(2)
	}

	cache := getCache()
	ensureCache(cache)
	inst := resolveInstanceOrExit(cache, target)

	fmt.Printf("🔗 启动隧道: 127.0.0.1:%d → %s:%d (via %s)\n", localPort, remoteHost, remotePort, inst.Name)

	// Self-exec: `tssh [--profile X] _portforward <instanceID> <localPort> <remotePort>`
	// Propagating --profile is essential: subprocess loads its own config and
	// without the flag would pick the default profile instead of the user's
	// current one.
	exe, _ := os.Executable()
	var cmdArgs []string
	if globalProfile != "" {
		cmdArgs = append(cmdArgs, "--profile", globalProfile)
	}
	cmdArgs = append(cmdArgs, "_portforward", inst.ID, strconv.Itoa(localPort), strconv.Itoa(remotePort))
	cmd := execCommand(exe, cmdArgs...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	// Credentials via env (secure)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ 启动失败: %v\n", err)
		os.Exit(1)
	}

	// Detach the child process
	cmd.Process.Release()

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
	// 0600 to match cache/history; tunnels reveal instance IDs + port maps.
	os.WriteFile(tunnelsFile(), data, 0600)
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
