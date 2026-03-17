package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const version = "1.2.5"

// Global flags parsed from os.Args before subcommand dispatch
var globalProfile string

func main() {
	// Detect command name for symlink-based dispatch (tscp, trsync)
	cmdName := filepath.Base(os.Args[0])
	switch cmdName {
	case "tscp":
		tscpMain()
		return
	case "trsync":
		trsyncMain()
		return
	}

	// Parse global flags (--profile) before subcommand
	args := os.Args[1:]
	var filteredArgs []string
	for i := 0; i < len(args); i++ {
		if (args[i] == "--profile" || args[i] == "-p") && i+1 < len(args) {
			globalProfile = args[i+1]
			i++
		} else {
			filteredArgs = append(filteredArgs, args[i])
		}
	}

	if len(filteredArgs) == 0 {
		cmdConnect("")
		return
	}

	switch filteredArgs[0] {
	case "ls", "list":
		cmdList(filteredArgs[1:])
	case "sync":
		cmdSync()
	case "exec":
		cmdExec(filteredArgs[1:])
	case "cp":
		cmdCopy(filteredArgs[1:])
	case "health":
		cmdHealth(filteredArgs[1:])
	case "ping":
		cmdPing(filteredArgs[1:])
	case "ssh-config":
		cmdSSHConfig()
	case "profiles":
		cmdProfiles()
	case "history":
		cmdHistory()
	case "completion":
		cmdCompletion()
	case "--complete":
		cmdComplete()
	case "version", "--version", "-v":
		fmt.Printf("tssh %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		// SSH-like: tssh [flags] <name> [command]
		target, localForward, command := parseSSHArgs(filteredArgs)

		if target != "" {
			if localForward != "" {
				cmdPortForward(target, localForward)
			} else if len(command) > 0 {
				cmdRemoteExec(target, strings.Join(command, " "))
			} else {
				cmdConnect(target)
			}
		} else {
			// No target found — maybe all args were flags; launch interactive selector
			if localForward == "" {
				cmdConnect("")
			} else {
				printUsage()
			}
		}
	}
}

// parseSSHArgs parses SSH-compatible flags from the argument list.
// Recognized flags are silently consumed; -L is captured for port forwarding.
// Remaining positional args become target and command.
func parseSSHArgs(args []string) (target, localForward string, command []string) {
	// SSH flags that take an argument (skip next arg)
	argFlags := map[string]bool{
		"-l": true, "-p": true, "-i": true, "-o": true,
		"-D": true, "-R": true, "-W": true, "-J": true,
		"-b": true, "-c": true, "-e": true, "-m": true,
		"-S": true, "-w": true, "-F": true, "-E": true,
		"-O": true, "-Q": true, "-B": true,
	}
	// SSH flags with no argument (just skip)
	boolFlags := map[string]bool{
		"-N": true, "-f": true, "-v": true, "-q": true,
		"-t": true, "-T": true, "-4": true, "-6": true,
		"-A": true, "-a": true, "-C": true, "-g": true,
		"-K": true, "-k": true, "-M": true, "-n": true,
		"-s": true, "-x": true, "-X": true, "-Y": true,
		"-vv": true, "-vvv": true,
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-L":
			if i+1 < len(args) {
				localForward = args[i+1]
				i++
			}
		case argFlags[arg]:
			// Skip this flag and its argument
			if i+1 < len(args) {
				i++
			}
		case boolFlags[arg]:
			// Skip boolean flag
		case strings.HasPrefix(arg, "-") && len(arg) > 1 && target == "":
			// Unknown flag — warn and skip
			fmt.Fprintf(os.Stderr, "⚠️  忽略不支持的 SSH 选项: %s\n", arg)
		default:
			if target == "" {
				target = arg
			} else {
				command = append(command, arg)
			}
		}
	}
	return
}

func printUsage() {
	fmt.Println(`tssh — 阿里云 ECS 快速连接工具 (v` + version + `)

用法 (像 ssh 一样):
  tssh <name>                       连接到指定机器
  tssh <name> <command>             远程执行命令
  tssh -L <local>:<host>:<remote> <name>   端口转发 (支持远程host)

全局选项:
  --profile, -p <name>   使用指定账号配置

子命令:
  tssh ls [-j] [--tag k=v]         列出实例
  tssh sync                        同步实例缓存
  tssh exec [options] <target> <cmd>   远程执行
  tssh cp [-g <pat>] <src> <dst>   文件拷贝
  tssh health [-g <pat>]           健康检查
  tssh ping [-g <pat>] [<name>]    连通性测试
  tssh ssh-config                  生成 SSH config
  tssh profiles                    列出所有账号
  tssh history                     查看执行历史

exec 选项:
  -g <keyword>     批量执行 (支持正则/多关键字/tag:key=val)
  -j, --json       JSON 输出
  -q, --quiet      安静模式
  -s, --script <f> 从文件执行
  --timeout <sec>  超时 (默认60s)
  --progress       显示进度
  --tag <k=v>      按标签过滤
  -                从 stdin 读取

配套工具:
  tscp / trsync`)
}

// cmdConnect connects interactively
func cmdConnect(target string) {
	cache := getCache()
	ensureCache(cache)

	inst := resolveInstance(cache, target)

	config := mustLoadConfig()

	fmt.Printf("🔗 连接: %s (%s / %s)\n", inst.Name, inst.ID, inst.PrivateIP)
	err := ConnectSession(config, inst.ID)
	fatal(err, "connect")
}

// cmdRemoteExec runs a command on a single instance (SSH-like)
func cmdRemoteExec(target, command string) {
	cache := getCache()
	inst := resolveInstance(cache, target)

	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	result, err := client.RunCommand(inst.ID, command, 60)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %s: %v\n", inst.Name, err)
		if result != nil {
			os.Exit(result.ExitCode)
		}
		os.Exit(1)
	}

	fmt.Print(decodeOutput(result.Output))
	os.Exit(result.ExitCode)
}

// cmdPortForward handles -L port forwarding with REMOTE HOST support
func cmdPortForward(target, spec string) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) != 3 {
		fmt.Fprintln(os.Stderr, "❌ 格式: -L <localPort>:<remoteHost>:<remotePort>")
		os.Exit(1)
	}

	localPort, err := strconv.Atoi(parts[0])
	fatal(err, "invalid local port")
	remoteHost := parts[1]
	remotePort, err := strconv.Atoi(parts[2])
	fatal(err, "invalid remote port")

	cache := getCache()
	inst := resolveInstance(cache, target)

	config := mustLoadConfig()

	fmt.Printf("🔗 %s (%s)\n", inst.Name, inst.ID)
	fmt.Printf("📡 端口转发: 127.0.0.1:%d → %s:%d\n", localPort, remoteHost, remotePort)

	if remoteHost == "localhost" || remoteHost == "127.0.0.1" {
		// Direct port forwarding via Cloud Assistant (original behavior)
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
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		fatal(cmd.Run(), "portforward")
	} else {
		// Remote host forwarding: use socat on the remote machine via portforward
		// First, set up portforward to a high port on the remote machine
		// Then use socat/ssh tunnel to reach the actual remote host
		fmt.Fprintf(os.Stderr, "📡 通过 %s 中转到 %s:%d\n", inst.Name, remoteHost, remotePort)

		// Strategy: portforward to an ephemeral port on the ECS instance,
		// then run socat on the ECS to relay to the actual remote host.
		// Step 1: Run socat in background on remote
		client, err := NewAliyunClient(config)
		fatal(err, "create client")

		socatPort := 19999
		socatCmd := fmt.Sprintf("nohup socat TCP-LISTEN:%d,fork,reuseaddr TCP:%s:%d &>/dev/null & echo $!", socatPort, remoteHost, remotePort)
		result, err := client.RunCommand(inst.ID, socatCmd, 10)
		if err != nil {
			// Try installing socat
			fmt.Fprintln(os.Stderr, "⚙️  安装 socat...")
			client.RunCommand(inst.ID, "which socat || (apt-get install -y socat 2>/dev/null || yum install -y socat 2>/dev/null)", 30)
			result, err = client.RunCommand(inst.ID, socatCmd, 10)
			fatal(err, "start socat")
		}
		socatPid := strings.TrimSpace(decodeOutput(result.Output))

		// Step 2: portforward to socat port
		cmdArgs := []string{
			"ali-instance-cli", "portforward",
			"--instance", inst.ID,
			"--local-port", strconv.Itoa(localPort),
			"--remote-port", strconv.Itoa(socatPort),
			"--region", config.Region,
			"--access-key-id", config.AccessKeyID,
			"--access-key-secret", config.AccessKeySecret,
		}
		cmd := execCommand(cmdArgs[0], cmdArgs[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		// Cleanup socat on exit
		defer func() {
			if socatPid != "" {
				client.RunCommand(inst.ID, fmt.Sprintf("kill %s 2>/dev/null", socatPid), 5)
			}
		}()

		fatal(cmd.Run(), "portforward")
	}
}

// cmdList prints all cached instances
func cmdList(args []string) {
	jsonMode := false
	tagFilter := ""
	searchPattern := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-j", "--json":
			jsonMode = true
		case "--tag":
			if i+1 < len(args) {
				tagFilter = args[i+1]
				i++
			}
		default:
			searchPattern = args[i]
		}
	}

	cache := getCache()
	ensureCache(cache)
	instances, err := cache.Load()
	fatal(err, "load cache")

	// Apply tag filter
	if tagFilter != "" {
		instances = FilterInstances(instances, "tag:"+tagFilter)
	}
	// Apply search pattern
	if searchPattern != "" {
		instances = FilterInstances(instances, searchPattern)
	}

	if jsonMode {
		data, _ := json.Marshal(instances)
		fmt.Println(string(data))
	} else {
		PrintInstanceList(instances)
	}
}

// cmdSync fetches all instances from Aliyun API
func cmdSync() {
	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	fmt.Fprintf(os.Stderr, "🔄 正在从阿里云拉取 ECS 实例列表 (profile: %s, region: %s)...\n", config.ProfileName, config.Region)
	instances, err := client.FetchAllInstances()
	fatal(err, "fetch instances")

	cache := getCache()
	err = cache.Save(instances)
	fatal(err, "save cache")

	fmt.Fprintf(os.Stderr, "✅ 缓存已保存 (%d 台实例)\n", len(instances))
}

// cmdSyncQuiet fetches instances without printing progress (for auto-sync)
func cmdSyncQuiet() {
	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	instances, err := client.FetchAllInstances()
	fatal(err, "fetch instances")

	cache := getCache()
	err = cache.Save(instances)
	fatal(err, "save cache")
}

// execOptions holds parsed flags for the exec command
type execOptions struct {
	grepMode   bool
	jsonMode   bool
	quietMode  bool
	progress   bool
	timeout    int
	scriptFile string
	stdinMode  bool
	tagFilter  string
	pattern    string
	targets    []string
	command    string
}

func parseExecArgs(args []string) *execOptions {
	opts := &execOptions{timeout: 60}
	var positional []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-g", "--grep":
			opts.grepMode = true
			if i+1 < len(args) {
				opts.pattern = args[i+1]
				i++
			}
		case "-j", "--json":
			opts.jsonMode = true
		case "-q", "--quiet":
			opts.quietMode = true
		case "--progress":
			opts.progress = true
		case "--timeout":
			if i+1 < len(args) {
				opts.timeout, _ = strconv.Atoi(args[i+1])
				i++
			}
		case "-s", "--script":
			if i+1 < len(args) {
				opts.scriptFile = args[i+1]
				i++
			}
		case "--tag":
			if i+1 < len(args) {
				opts.tagFilter = args[i+1]
				i++
			}
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) > 0 && positional[len(positional)-1] == "-" {
		opts.stdinMode = true
		positional = positional[:len(positional)-1]
	}

	opts.targets = positional
	return opts
}

// cmdExec runs commands on one or more instances via Cloud Assistant
func cmdExec(args []string) {
	opts := parseExecArgs(args)

	// Determine command
	command := opts.command
	if opts.scriptFile != "" {
		data, err := os.ReadFile(opts.scriptFile)
		fatal(err, "read script file")
		command = string(data)
	} else if opts.stdinMode || !isTerminal() {
		data, err := io.ReadAll(os.Stdin)
		fatal(err, "read stdin")
		command = string(data)
	}

	if command == "" {
		if opts.grepMode {
			if len(opts.targets) < 1 {
				fmt.Fprintln(os.Stderr, "用法: tssh exec -g <keyword> <command>")
				os.Exit(1)
			}
			command = strings.Join(opts.targets, " ")
		} else {
			if len(opts.targets) < 2 {
				fmt.Fprintln(os.Stderr, "用法: tssh exec <name> <command>")
				fmt.Fprintln(os.Stderr, "      tssh exec -g <pattern> <command>")
				fmt.Fprintln(os.Stderr, "      echo 'script' | tssh exec <name> -")
				os.Exit(1)
			}
			command = strings.Join(opts.targets[1:], " ")
		}
	}

	if command == "" {
		fmt.Fprintln(os.Stderr, "❌ 没有指定要执行的命令")
		os.Exit(1)
	}

	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	cache := getCache()
	ensureCache(cache)

	var targets []Instance

	if opts.tagFilter != "" {
		// Tag-based targeting
		instances, _ := cache.Load()
		targets = FilterInstances(instances, "tag:"+opts.tagFilter)
	} else if opts.grepMode {
		targets, _ = cache.FindByPattern(opts.pattern)
	} else {
		targetName := ""
		if len(opts.targets) > 0 {
			targetName = opts.targets[0]
		}
		inst := resolveInstance(cache, targetName)
		targets = []Instance{*inst}
	}

	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "❌ 没有匹配的实例")
		os.Exit(1)
	}

	if !opts.quietMode && !opts.jsonMode {
		fmt.Fprintf(os.Stderr, "🚀 在 %d 台机器上执行: %s\n\n", len(targets), truncateStr(command, 80))
	}

	type execResult struct {
		Name     string `json:"name"`
		IP       string `json:"ip"`
		Output   string `json:"output"`
		Error    string `json:"error,omitempty"`
		ExitCode int    `json:"exit_code"`
		Skipped  bool   `json:"skipped,omitempty"`
	}
	results := make([]execResult, len(targets))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	var doneCount int64

	for i, inst := range targets {
		if inst.Status != "Running" {
			results[i] = execResult{Name: inst.Name, Skipped: true}
			atomic.AddInt64(&doneCount, 1)
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, inst Instance) {
			defer wg.Done()
			defer func() { <-sem }()
			result, err := client.RunCommand(inst.ID, command, opts.timeout)
			r := execResult{Name: inst.Name, IP: inst.PrivateIP}
			if err != nil {
				r.Error = err.Error()
				if result != nil {
					r.ExitCode = result.ExitCode
					r.Output = decodeOutput(result.Output)
				} else {
					r.ExitCode = 1
				}
			} else {
				r.Output = decodeOutput(result.Output)
				r.ExitCode = result.ExitCode
			}
			results[idx] = r

			done := atomic.AddInt64(&doneCount, 1)
			if opts.progress && !opts.jsonMode {
				fmt.Fprintf(os.Stderr, "\r⏳ [%d/%d] %s", done, len(targets), inst.Name)
			}
		}(i, inst)
	}
	wg.Wait()

	if opts.progress && !opts.jsonMode {
		fmt.Fprintf(os.Stderr, "\r✅ [%d/%d] 全部完成\n\n", len(targets), len(targets))
	}

	// Save to history
	saveHistory(command, results)

	// Output results
	if opts.jsonMode {
		data, _ := json.Marshal(results)
		fmt.Println(string(data))
	} else {
		maxExitCode := 0
		for _, r := range results {
			if r.Skipped {
				if !opts.quietMode {
					fmt.Printf("⛔ %s: skipped (not running)\n", r.Name)
				}
				continue
			}
			if !opts.quietMode {
				fmt.Printf("━━━ %s (%s) [exit:%d]\n", r.Name, r.IP, r.ExitCode)
			}
			if r.Error != "" {
				fmt.Fprintf(os.Stderr, "❌ Error: %s\n", r.Error)
			}
			if r.Output != "" {
				fmt.Print(r.Output)
			}
			if !opts.quietMode {
				fmt.Println()
			}
			if r.ExitCode > maxExitCode {
				maxExitCode = r.ExitCode
			}
		}
		if maxExitCode > 0 {
			os.Exit(maxExitCode)
		}
	}
}

// cmdCopy copies files to/from instances, supports -g for batch
func cmdCopy(args []string) {
	grepMode := false
	pattern := ""
	var remaining []string
	for i := 0; i < len(args); i++ {
		if (args[i] == "-g" || args[i] == "--grep") && i+1 < len(args) {
			grepMode = true
			pattern = args[i+1]
			i++
		} else {
			remaining = append(remaining, args[i])
		}
	}

	if len(remaining) < 2 {
		fmt.Println("用法: tssh cp <local> <name>:<remote>  (上传)")
		fmt.Println("      tssh cp <name>:<remote> <local>  (下载)")
		fmt.Println("      tssh cp -g <pattern> <local> :<remote>  (批量上传)")
		os.Exit(1)
	}

	if grepMode {
		doBatchCopy(pattern, remaining[0], remaining[1])
	} else {
		doCopy(remaining[0], remaining[1])
	}
}

// doBatchCopy uploads a file to multiple instances
func doBatchCopy(pattern, localPath, remoteDst string) {
	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")
	cache := getCache()
	ensureCache(cache)

	targets, _ := cache.FindByPattern(pattern)
	if len(targets) == 0 {
		fmt.Println("❌ 没有匹配的实例")
		os.Exit(1)
	}

	remotePath := remoteDst
	if strings.HasPrefix(remotePath, ":") {
		remotePath = remotePath[1:]
	}

	dir := filepath.Dir(remotePath)
	fileName := filepath.Base(localPath)
	if !strings.HasSuffix(remotePath, "/") && filepath.Base(remotePath) != "" {
		fileName = filepath.Base(remotePath)
		dir = filepath.Dir(remotePath)
	}

	// Check file size for large file support
	fileInfo, err := os.Stat(localPath)
	fatal(err, "stat file")

	if fileInfo.Size() > 32*1024 {
		fmt.Fprintf(os.Stderr, "⚠️  文件 %s (%dKB) 超过 32KB，使用 portforward+scp 模式\n", localPath, fileInfo.Size()/1024)
		// Large file: use portforward+scp for each target
		for _, inst := range targets {
			if inst.Status != "Running" {
				fmt.Printf("⛔ %s: skipped\n", inst.Name)
				continue
			}
			fmt.Printf("⬆️  %s → %s:%s\n", localPath, inst.Name, remotePath)
			err := scpViaPortforward(config, inst.ID, localPath, remotePath)
			if err != nil {
				fmt.Printf("❌ %s: %v\n", inst.Name, err)
			} else {
				fmt.Printf("✅ %s\n", inst.Name)
			}
		}
		return
	}

	fmt.Printf("⬆️  批量上传 %s → %d 台机器:%s/%s\n\n", localPath, len(targets), dir, fileName)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	type copyResult struct {
		name string
		err  error
	}
	results := make([]copyResult, len(targets))

	for i, inst := range targets {
		if inst.Status != "Running" {
			results[i] = copyResult{name: inst.Name, err: fmt.Errorf("not running")}
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, inst Instance) {
			defer wg.Done()
			defer func() { <-sem }()
			err := client.SendFile(inst.ID, localPath, dir, fileName)
			results[idx] = copyResult{name: inst.Name, err: err}
		}(i, inst)
	}
	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			fmt.Printf("❌ %s: %v\n", r.name, r.err)
		} else {
			fmt.Printf("✅ %s\n", r.name)
		}
	}
}

// scpViaPortforward uploads a large file via portforward+scp
func scpViaPortforward(config *Config, instanceID, localPath, remotePath string) error {
	localPort := findFreePort()

	pf := execCommand("ali-instance-cli", "portforward",
		"--instance", instanceID,
		"--remote-port", "22",
		"--local-port", strconv.Itoa(localPort),
		"--region", config.Region,
		"--access-key-id", config.AccessKeyID,
		"--access-key-secret", config.AccessKeySecret,
	)
	pf.Stderr = nil
	pf.Stdout = nil
	if err := pf.Start(); err != nil {
		return fmt.Errorf("portforward failed: %w", err)
	}
	defer func() {
		pf.Process.Kill()
		pf.Wait()
	}()

	sleepMs(3000)

	homeDir, _ := os.UserHomeDir()
	sshKey := filepath.Join(homeDir, ".ssh", "id_rsa")

	scp := execCommand("scp",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "PubkeyAcceptedAlgorithms=+ssh-rsa",
		"-o", "HostKeyAlgorithms=+ssh-rsa",
		"-o", "LogLevel=ERROR",
		"-i", sshKey,
		"-P", strconv.Itoa(localPort),
		localPath, fmt.Sprintf("root@127.0.0.1:%s", remotePath),
	)
	return scp.Run()
}

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

// cmdPing tests Cloud Assistant connectivity by running a simple echo command
func cmdPing(args []string) {
	pattern := ""
	var targets []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-g", "--grep":
			if i+1 < len(args) {
				pattern = args[i+1]
				i++
			}
		default:
			targets = append(targets, args[i])
		}
	}

	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	cache := getCache()
	ensureCache(cache)

	var instances []Instance

	if pattern != "" {
		instances, _ = cache.FindByPattern(pattern)
	} else if len(targets) > 0 {
		for _, t := range targets {
			inst := resolveInstance(cache, t)
			instances = append(instances, *inst)
		}
	} else {
		// Interactive select
		allInst, _ := cache.Load()
		inst, err := FuzzySelect(allInst, "")
		if err != nil {
			os.Exit(0)
		}
		instances = append(instances, *inst)
	}

	if len(instances) == 0 {
		fmt.Fprintln(os.Stderr, "❌ 没有匹配的实例")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "🏓 Ping %d 台机器...\n\n", len(instances))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	type pingResult struct {
		Name    string
		IP      string
		OK      bool
		Latency time.Duration
		Error   string
	}
	results := make([]pingResult, len(instances))

	for i, inst := range instances {
		if inst.Status != "Running" {
			results[i] = pingResult{Name: inst.Name, IP: inst.PrivateIP, Error: "not running"}
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, inst Instance) {
			defer wg.Done()
			defer func() { <-sem }()
			start := time.Now()
			result, err := client.RunCommand(inst.ID, "echo pong", 10)
			elapsed := time.Since(start)
			r := pingResult{Name: inst.Name, IP: inst.PrivateIP, Latency: elapsed}
			if err != nil {
				r.Error = err.Error()
			} else if result.ExitCode != 0 {
				r.Error = fmt.Sprintf("exit %d", result.ExitCode)
			} else {
				r.OK = true
			}
			results[idx] = r
		}(i, inst)
	}
	wg.Wait()

	okCount := 0
	failCount := 0
	nameW := 25
	for _, r := range results {
		if len(r.Name) > nameW {
			nameW = len(r.Name)
		}
	}
	if nameW > 40 {
		nameW = 40
	}

	for _, r := range results {
		if r.OK {
			okCount++
			fmt.Printf("✅ %-*s  %s  %dms\n", nameW, shortenName(r.Name, nameW), r.IP, r.Latency.Milliseconds())
		} else {
			failCount++
			fmt.Printf("❌ %-*s  %s  %s\n", nameW, shortenName(r.Name, nameW), r.IP, r.Error)
		}
	}

	fmt.Fprintf(os.Stderr, "\n📊 %d 成功, %d 失败\n", okCount, failCount)
	if failCount > 0 {
		os.Exit(1)
	}
}

// shortenName truncates a name to maxLen, preserving meaningful parts
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

// cmdSSHConfig generates SSH config entries using portforward
func cmdSSHConfig() {
	cache := getCache()
	ensureCache(cache)
	instances, err := cache.Load()
	fatal(err, "load cache")

	config := mustLoadConfig()

	fmt.Println("# Generated by tssh ssh-config")
	fmt.Printf("# Profile: %s, Region: %s\n", config.ProfileName, config.Region)
	fmt.Printf("# %d instances, %s\n\n", len(instances), time.Now().Format("2006-01-02 15:04:05"))

	for _, inst := range instances {
		if inst.Status != "Running" {
			continue
		}
		fmt.Printf("Host %s\n", inst.Name)
		fmt.Printf("  HostName %s\n", inst.PrivateIP)
		fmt.Printf("  User root\n")
		fmt.Printf("  # InstanceID: %s\n", inst.ID)
		fmt.Printf("  # Use: tssh %s\n\n", inst.Name)
	}
}

// cmdProfiles lists available profiles
func cmdProfiles() {
	profiles := ListProfiles()
	if len(profiles) == 0 {
		fmt.Println("没有找到配置的账号")
		return
	}
	fmt.Println("可用账号:")
	for _, p := range profiles {
		marker := "  "
		if p == globalProfile || (globalProfile == "" && (p == "default" || p == "env")) {
			marker = "→ "
		}
		fmt.Printf("  %s%s\n", marker, p)
	}
}

// cmdHistory shows recent exec history
func cmdHistory() {
	cache := getCache()
	histFile := filepath.Join(cache.HistoryDir(), "history.json")
	data, err := os.ReadFile(histFile)
	if err != nil {
		fmt.Println("暂无执行历史")
		return
	}

	var entries []historyEntry
	json.Unmarshal(data, &entries)

	// Show last 20
	start := 0
	if len(entries) > 20 {
		start = len(entries) - 20
	}
	for _, e := range entries[start:] {
		fmt.Printf("[%s] %d台 → %s\n", e.Time, e.TargetCount, truncateStr(e.Command, 60))
	}
}

type historyEntry struct {
	Time        string `json:"time"`
	Command     string `json:"command"`
	TargetCount int    `json:"target_count"`
	Profile     string `json:"profile,omitempty"`
}

func saveHistory(command string, results interface{}) {
	cache := getCache()
	histFile := filepath.Join(cache.HistoryDir(), "history.json")

	var entries []historyEntry
	if data, err := os.ReadFile(histFile); err == nil {
		json.Unmarshal(data, &entries)
	}

	count := 0
	if r, ok := results.([]struct{ Name string }); ok {
		count = len(r)
	}
	// Use reflection-free approach
	switch v := results.(type) {
	case []interface{}:
		count = len(v)
	default:
		// Try to get length via JSON re-encoding
		if data, err := json.Marshal(results); err == nil {
			var arr []json.RawMessage
			if json.Unmarshal(data, &arr) == nil {
				count = len(arr)
			}
		}
	}

	entries = append(entries, historyEntry{
		Time:        time.Now().Format("2006-01-02 15:04:05"),
		Command:     command,
		TargetCount: count,
		Profile:     globalProfile,
	})

	// Keep last 1000 entries
	if len(entries) > 1000 {
		entries = entries[len(entries)-1000:]
	}

	cache.Ensure()
	data, _ := json.Marshal(entries)
	os.WriteFile(histFile, data, 0644)
}

// --- Symlink tools ---

func tscpMain() {
	if len(os.Args) < 3 {
		fmt.Println("tscp — 阿里云 ECS 文件拷贝工具")
		fmt.Println("\n用法:")
		fmt.Println("  tscp <local> <name>:<remote>   上传")
		fmt.Println("  tscp <name>:<remote> <local>   下载")
		return
	}
	doCopy(os.Args[1], os.Args[2])
}

func trsyncMain() {
	if len(os.Args) < 3 {
		fmt.Println("trsync — 阿里云 ECS rsync 同步工具")
		fmt.Println("\n用法:")
		fmt.Println("  trsync <local_dir> <name>:<remote_dir>   上传同步")
		fmt.Println("  trsync <name>:<remote_dir> <local_dir>   下载同步")
		return
	}

	src := os.Args[1]
	dst := os.Args[2]

	config := mustLoadConfig()
	cache := getCache()
	ensureCache(cache)

	var name, remotePath, localPath string
	var upload bool

	if strings.Contains(dst, ":") {
		upload = true
		name = dst[:strings.Index(dst, ":")]
		remotePath = dst[strings.Index(dst, ":")+1:]
		localPath = src
	} else if strings.Contains(src, ":") {
		upload = false
		name = src[:strings.Index(src, ":")]
		remotePath = src[strings.Index(src, ":")+1:]
		localPath = dst
	} else {
		fmt.Println("❌ 需要用 name:path 格式指定远程路径")
		os.Exit(1)
	}

	inst := resolveInstance(cache, name)

	localPort := findFreePort()
	fmt.Printf("🔗 %s → portforward :%d\n", inst.Name, localPort)

	pf := execCommand("ali-instance-cli", "portforward",
		"--instance", inst.ID,
		"--remote-port", "21022",
		"--local-port", strconv.Itoa(localPort),
		"--region", config.Region,
		"--access-key-id", config.AccessKeyID,
		"--access-key-secret", config.AccessKeySecret,
	)
	pf.Stderr = nil
	pf.Stdout = nil
	if err := pf.Start(); err != nil {
		pf = execCommand("ali-instance-cli", "portforward",
			"--instance", inst.ID,
			"--remote-port", "22",
			"--local-port", strconv.Itoa(localPort),
			"--region", config.Region,
			"--access-key-id", config.AccessKeyID,
			"--access-key-secret", config.AccessKeySecret,
		)
		pf.Stderr = nil
		pf.Stdout = nil
		fatal(pf.Start(), "portforward")
	}
	defer func() {
		pf.Process.Kill()
		pf.Wait()
	}()

	sleepMs(4000)

	homeDir, _ := os.UserHomeDir()
	sshKey := filepath.Join(homeDir, ".ssh", "id_rsa")
	sshOpts := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o PubkeyAcceptedAlgorithms=+ssh-rsa -o HostKeyAlgorithms=+ssh-rsa -o LogLevel=ERROR -i %s -p %d", sshKey, localPort)

	var rsyncArgs []string
	if upload {
		fmt.Printf("⬆️  rsync %s → %s:%s\n", localPath, inst.Name, remotePath)
		rsyncArgs = []string{"-avz", "--progress", "-e", sshOpts, localPath, fmt.Sprintf("root@127.0.0.1:%s", remotePath)}
	} else {
		fmt.Printf("⬇️  rsync %s:%s → %s\n", inst.Name, remotePath, localPath)
		rsyncArgs = []string{"-avz", "--progress", "-e", sshOpts, fmt.Sprintf("root@127.0.0.1:%s", remotePath), localPath}
	}

	rsync := execCommand("rsync", rsyncArgs...)
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	rsync.Stdin = os.Stdin
	err := rsync.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ rsync failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 完成")
}

func doCopy(src, dst string) {
	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")
	cache := getCache()
	ensureCache(cache)

	if strings.Contains(dst, ":") {
		// Upload
		name := dst[:strings.Index(dst, ":")]
		remotePath := dst[strings.Index(dst, ":")+1:]
		inst := resolveInstance(cache, name)

		dir := filepath.Dir(remotePath)
		fileName := filepath.Base(src)
		if !strings.HasSuffix(remotePath, "/") && filepath.Base(remotePath) != "" {
			fileName = filepath.Base(remotePath)
			dir = filepath.Dir(remotePath)
		}

		// Check file size
		fileInfo, statErr := os.Stat(src)
		if statErr == nil && fileInfo.Size() > 32*1024 {
			fmt.Fprintf(os.Stderr, "⬆️  %s (%dKB) → %s:%s (大文件, portforward+scp)\n", src, fileInfo.Size()/1024, inst.Name, remotePath)
			err := scpViaPortforward(config, inst.ID, src, remotePath)
			fatal(err, "scp upload")
			fmt.Println("✅ 完成")
			return
		}

		fmt.Printf("⬆️  %s → %s:%s/%s\n", src, inst.Name, dir, fileName)
		err = client.SendFile(inst.ID, src, dir, fileName)
		fatal(err, "upload")
		fmt.Println("✅ 完成")

	} else if strings.Contains(src, ":") {
		// Download
		name := src[:strings.Index(src, ":")]
		remotePath := src[strings.Index(src, ":")+1:]
		inst := resolveInstance(cache, name)

		fmt.Printf("⬇️  %s:%s → %s\n", inst.Name, remotePath, dst)
		result, err := client.RunCommand(inst.ID, fmt.Sprintf("base64 '%s'", remotePath), 60)
		fatal(err, "download")

		outerDecoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(result.Output))
		fatal(err, "decode outer")
		innerDecoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(outerDecoded)))
		fatal(err, "decode inner")

		err = os.WriteFile(dst, innerDecoded, 0644)
		fatal(err, "write file")
		fmt.Println("✅ 完成")
	} else {
		fmt.Println("❌ 需要用 name:path 格式指定远程路径")
		os.Exit(1)
	}
}

// --- Shell completion ---

func cmdCompletion() {
	shell := os.Getenv("SHELL")
	exe, _ := os.Executable()

	if strings.Contains(shell, "zsh") {
		fmt.Printf(`# 添加到 ~/.zshrc:
_tssh() {
  local -a instances
  instances=(${(f)"$(%s --complete)"})
  _describe 'instances' instances
}
compdef _tssh tssh
compdef _tssh tscp
compdef _tssh trsync
`, exe)
	} else {
		fmt.Printf(`# 添加到 ~/.bashrc:
_tssh() {
  local cur=${COMP_WORDS[COMP_CWORD]}
  COMPREPLY=($(compgen -W "$(%s --complete)" -- "$cur"))
}
complete -F _tssh tssh
complete -F _tssh tscp
complete -F _tssh trsync
`, exe)
	}
}

func cmdComplete() {
	cache := getCache()
	instances, err := cache.Load()
	if err != nil {
		return
	}
	for _, inst := range instances {
		if inst.Name != "" {
			fmt.Println(inst.Name)
		}
	}
}

// --- Helpers ---

func getCache() *Cache {
	if globalProfile != "" {
		return NewCacheWithProfile(globalProfile)
	}
	return NewCache()
}

func mustLoadConfig() *Config {
	config, err := LoadConfigWithProfile(globalProfile)
	fatal(err, "load config")
	return config
}

func ensureCache(cache *Cache) {
	if !cache.Exists() {
		fmt.Fprintln(os.Stderr, "⚠️  缓存不存在，正在同步...")
		cmdSyncQuiet()
		return
	}

	if cache.Age() > 24*time.Hour {
		fmt.Fprintln(os.Stderr, "⚠️  缓存已过期 (>24h)，后台刷新中...")
		go func() {
			config, err := LoadConfigWithProfile(globalProfile)
			if err != nil {
				return
			}
			client, err := NewAliyunClient(config)
			if err != nil {
				return
			}
			instances, err := client.FetchAllInstances()
			if err != nil {
				return
			}
			cache.Save(instances)
			// Don't print here — would corrupt interactive UI
		}()
	}
}

func resolveInstance(cache *Cache, name string) *Instance {
	ensureCache(cache)
	instances, err := cache.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 加载缓存失败: %v\n", err)
		os.Exit(1)
	}

	if name == "" {
		inst, err := FuzzySelect(instances, "")
		if err != nil {
			os.Exit(0)
		}
		return inst
	}

	if idx, err := strconv.Atoi(name); err == nil && idx >= 1 && idx <= len(instances) {
		return &instances[idx-1]
	}

	inst, err := cache.FindByName(name)
	if err == nil {
		return inst
	}

	matches, _ := cache.FindByPattern(name)
	if len(matches) == 1 {
		return &matches[0]
	}

	if len(matches) > 1 {
		fmt.Fprintf(os.Stderr, "⚠️  '%s' 匹配了 %d 台，请选择:\n", name, len(matches))
		inst, err := FuzzySelect(instances, name)
		if err != nil {
			os.Exit(0)
		}
		return inst
	}

	fmt.Fprintf(os.Stderr, "❌ 找不到 '%s'\n", name)
	os.Exit(1)
	return nil
}

func decodeOutput(output string) string {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(output))
	if err != nil {
		return output
	}
	return string(decoded)
}

func truncateStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return true
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func fatal(err error, msg string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %s: %v\n", msg, err)
		os.Exit(1)
	}
}
