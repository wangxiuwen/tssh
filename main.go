package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const version = "1.0.0"

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

	if len(os.Args) < 2 {
		// No args → interactive search & connect
		cmdConnect("")
		return
	}

	switch os.Args[1] {
	case "ls", "list":
		cmdList()
	case "sync":
		cmdSync()
	case "exec":
		cmdExec()
	case "cp":
		cmdCopy()
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
		args := os.Args[1:]
		localForward := ""
		target := ""
		var command []string

		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "-L":
				if i+1 < len(args) {
					localForward = args[i+1]
					i++
				}
			default:
				if target == "" {
					target = args[i]
				} else {
					command = append(command, args[i])
				}
			}
		}

		if target != "" {
			if localForward != "" {
				cmdPortForward(target, localForward)
			} else if len(command) > 0 {
				cmdRemoteExec(target, strings.Join(command, " "))
			} else {
				cmdConnect(target)
			}
		} else {
			printUsage()
		}
	}
}

func printUsage() {
	fmt.Println(`tssh — 阿里云 ECS 快速连接工具 (v` + version + `)

用法 (像 ssh 一样):
  tssh <name>                       连接到指定机器
  tssh <name> <command>             远程执行命令
  tssh -L <local>:<host>:<remote> <name>   端口转发

子命令:
  tssh ls                           列出所有实例
  tssh sync                         同步实例缓存
  tssh exec <name> <command>        远程执行命令 (Cloud Assistant)
  tssh exec -g <keyword> <command>  批量执行
  tssh completion                   安装 shell Tab 补全

配套工具:
  tscp <local> <name>:<remote>      上传文件
  tscp <name>:<remote> <local>      下载文件
  trsync <local> <name>:<remote>    rsync 同步`)
}

// cmdConnect connects interactively using ali-instance-cli session
func cmdConnect(target string) {
	cache := NewCache()
	ensureCache(cache)

	inst := resolveInstance(cache, target)

	config, err := LoadConfig()
	fatal(err, "load config")

	fmt.Printf("🔗 连接: %s (%s / %s)\n", inst.Name, inst.ID, inst.PrivateIP)
	err = ConnectSession(config, inst.ID)
	fatal(err, "connect")
}

// cmdRemoteExec runs a command on a single instance (SSH-like: tssh host cmd)
func cmdRemoteExec(target, command string) {
	cache := NewCache()
	inst := resolveInstance(cache, target)

	config, err := LoadConfig()
	fatal(err, "load config")
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	output, err := client.RunCommand(inst.ID, command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %s: %v\n", inst.Name, err)
		os.Exit(1)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(output))
	if err != nil {
		fmt.Print(output)
	} else {
		fmt.Print(string(decoded))
	}
}

// cmdPortForward handles -L port forwarding
func cmdPortForward(target, spec string) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) != 3 {
		fmt.Fprintln(os.Stderr, "❌ 格式: -L <localPort>:<host>:<remotePort>")
		os.Exit(1)
	}

	localPort, err := strconv.Atoi(parts[0])
	fatal(err, "invalid local port")
	remotePort, err := strconv.Atoi(parts[2])
	fatal(err, "invalid remote port")

	cache := NewCache()
	inst := resolveInstance(cache, target)

	config, err := LoadConfig()
	fatal(err, "load config")

	fmt.Printf("🔗 %s (%s)\n", inst.Name, inst.ID)
	fmt.Printf("📡 端口转发: 127.0.0.1:%d → localhost:%d\n", localPort, remotePort)

	// Use ali-instance-cli portforward
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
}

// cmdList prints all cached instances
func cmdList() {
	cache := NewCache()
	ensureCache(cache)
	instances, err := cache.Load()
	fatal(err, "load cache")
	PrintInstanceList(instances)
}

// cmdSync fetches all instances from Aliyun API
func cmdSync() {
	config, err := LoadConfig()
	fatal(err, "load config")
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	fmt.Println("🔄 正在从阿里云拉取 ECS 实例列表...")
	instances, err := client.FetchAllInstances()
	fatal(err, "fetch instances")

	cache := NewCache()
	err = cache.Save(instances)
	fatal(err, "save cache")

	fmt.Printf("✅ 缓存已保存 (%d 台实例)\n", len(instances))
}

// cmdExec runs commands on one or more instances via Cloud Assistant
func cmdExec() {
	args := os.Args[2:]
	if len(args) < 2 {
		fmt.Println("用法: tssh exec <name> <command>")
		fmt.Println("      tssh exec -g <keyword> <command>")
		os.Exit(1)
	}

	config, err := LoadConfig()
	fatal(err, "load config")
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	cache := NewCache()
	ensureCache(cache)

	var targets []Instance
	var command string

	if args[0] == "-g" || args[0] == "--grep" {
		if len(args) < 3 {
			fmt.Println("用法: tssh exec -g <keyword> <command>")
			os.Exit(1)
		}
		targets, _ = cache.FindByPattern(args[1])
		command = strings.Join(args[2:], " ")
	} else {
		inst := resolveInstance(cache, args[0])
		targets = []Instance{*inst}
		command = strings.Join(args[1:], " ")
	}

	if len(targets) == 0 {
		fmt.Println("❌ 没有匹配的实例")
		os.Exit(1)
	}

	fmt.Printf("🚀 在 %d 台机器上执行: %s\n\n", len(targets), command)

	for _, inst := range targets {
		if inst.Status != "Running" {
			fmt.Printf("⛔ %s: skipped (not running)\n", inst.Name)
			continue
		}

		output, err := client.RunCommand(inst.ID, command)
		fmt.Printf("━━━ %s (%s)\n", inst.Name, inst.PrivateIP)
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		} else {
			decoded, e := base64.StdEncoding.DecodeString(strings.TrimSpace(output))
			if e != nil {
				fmt.Print(output)
			} else {
				fmt.Print(string(decoded))
			}
		}
		fmt.Println()
	}
}

// cmdCopy copies files to/from instances
func cmdCopy() {
	args := os.Args[2:]
	if len(args) < 2 {
		fmt.Println("用法: tssh cp <local> <name>:<remote>  (上传)")
		fmt.Println("      tssh cp <name>:<remote> <local>  (下载)")
		os.Exit(1)
	}
	doCopy(args[0], args[1])
}

// tscpMain handles `tscp` command (symlink dispatch)
func tscpMain() {
	if len(os.Args) < 3 {
		fmt.Println("tscp — 阿里云 ECS 文件拷贝工具")
		fmt.Println()
		fmt.Println("用法:")
		fmt.Println("  tscp <local> <name>:<remote>   上传")
		fmt.Println("  tscp <name>:<remote> <local>   下载")
		return
	}
	doCopy(os.Args[1], os.Args[2])
}

// trsyncMain handles `trsync` command
func trsyncMain() {
	if len(os.Args) < 3 {
		fmt.Println("trsync — 阿里云 ECS rsync 同步工具")
		fmt.Println()
		fmt.Println("用法:")
		fmt.Println("  trsync <local_dir> <name>:<remote_dir>   上传同步")
		fmt.Println("  trsync <name>:<remote_dir> <local_dir>   下载同步")
		return
	}

	src := os.Args[1]
	dst := os.Args[2]

	config, err := LoadConfig()
	fatal(err, "load config")
	cache := NewCache()
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

	// Use ali-instance-cli portforward + rsync
	client, err := NewAliyunClient(config)
	_ = client // for future use

	localPort := findFreePort()

	fmt.Printf("🔗 %s → portforward :%d\n", inst.Name, localPort)

	// Start portforward in background
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
		// Try port 22
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

	// Wait for tunnel
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
	err = rsync.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ rsync failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 完成")
}

func doCopy(src, dst string) {
	config, err := LoadConfig()
	fatal(err, "load config")
	client, err := NewAliyunClient(config)
	fatal(err, "create client")
	cache := NewCache()
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

		fmt.Printf("⬆️  %s → %s:%s/%s\n", src, inst.Name, dir, fileName)
		err = client.SendFile(inst.ID, src, dir, fileName)
		fatal(err, "upload")
		fmt.Println("✅ 完成")

	} else if strings.Contains(src, ":") {
		// Download via RunCommand + base64
		name := src[:strings.Index(src, ":")]
		remotePath := src[strings.Index(src, ":")+1:]
		inst := resolveInstance(cache, name)

		fmt.Printf("⬇️  %s:%s → %s\n", inst.Name, remotePath, dst)
		output, err := client.RunCommand(inst.ID, fmt.Sprintf("base64 '%s'", remotePath))
		fatal(err, "download")

		// RunCommand output is base64(base64(file_content))
		outerDecoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(output))
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

// cmdCompletion outputs shell completion setup
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
	cache := NewCache()
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

func ensureCache(cache *Cache) {
	if !cache.Exists() {
		fmt.Println("⚠️  缓存不存在，正在同步...")
		cmdSync()
	}
}

func resolveInstance(cache *Cache, name string) *Instance {
	ensureCache(cache)
	instances, err := cache.Load()
	if err != nil {
		fmt.Printf("❌ 加载缓存失败: %v\n", err)
		os.Exit(1)
	}

	if name == "" {
		inst, err := FuzzySelect(instances, "")
		if err != nil {
			os.Exit(0)
		}
		return inst
	}

	// Try as exact exact index first
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
		fmt.Printf("⚠️  '%s' 匹配了 %d 台，请选择:\n", name, len(matches))
		inst, err := FuzzySelect(instances, name)
		if err != nil {
			os.Exit(0)
		}
		return inst
	}

	fmt.Printf("❌ 找不到 '%s'\n", name)
	os.Exit(1)
	return nil
}

func fatal(err error, msg string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %s: %v\n", msg, err)
		os.Exit(1)
	}
}
