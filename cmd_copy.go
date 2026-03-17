package main

import (
"encoding/base64"
"fmt"
"os"
"path/filepath"
"strconv"
"strings"
"sync"
)


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
