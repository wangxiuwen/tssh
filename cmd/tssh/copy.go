package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// chunkSize is the per-call payload size for chunked transfers. Cloud Assistant
// limits SendFile Content (base64) to ~32KB and RunCommand Output (base64) to
// ~24KB after wrapping, so 16KB raw per chunk leaves comfortable headroom on
// both directions.
const chunkSize = 16 * 1024

// chunkedUpload streams a local file to the remote in 16KB chunks via SendFile,
// then concatenates them into the target path. Works for arbitrarily large
// files without depending on scp / ssh keys.
func chunkedUpload(client *AliyunClient, instanceID, localPath, remoteDir, remoteFileName string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local: %w", err)
	}
	total := len(data)
	if total == 0 {
		return client.SendFileContent(instanceID, data, remoteDir, remoteFileName)
	}

	session := fmt.Sprintf("tssh-cp-%d", time.Now().UnixNano())
	tmpDir := "/tmp/" + session
	if _, err := client.RunCommand(instanceID, fmt.Sprintf("mkdir -p '%s' '%s'", tmpDir, shellQuote(remoteDir)), 15); err != nil {
		return fmt.Errorf("prepare remote dirs: %w", err)
	}
	cleanup := func() {
		client.RunCommand(instanceID, fmt.Sprintf("rm -rf '%s'", tmpDir), 15)
	}
	defer cleanup()

	nChunks := (total + chunkSize - 1) / chunkSize
	for i := 0; i < nChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > total {
			end = total
		}
		partName := fmt.Sprintf("part-%06d", i)
		if err := client.SendFileContent(instanceID, data[start:end], tmpDir, partName); err != nil {
			return fmt.Errorf("upload chunk %d/%d: %w", i+1, nChunks, err)
		}
		fmt.Fprintf(os.Stderr, "\r📤 上传中 %d/%d (%d/%d KB)", i+1, nChunks, end/1024, total/1024)
	}
	fmt.Fprintln(os.Stderr)

	target := filepath.Join(remoteDir, remoteFileName)
	assemble := fmt.Sprintf("cat '%s'/part-* > '%s' && stat -c%%s '%s'", tmpDir, target, target)
	res, err := client.RunCommand(instanceID, assemble, 60)
	if err != nil {
		return fmt.Errorf("assemble remote: %w", err)
	}
	gotStr := strings.TrimSpace(decodeOutput(res.Output))
	got, _ := strconv.Atoi(gotStr)
	if got != total {
		return fmt.Errorf("size mismatch: local=%d remote=%s", total, gotStr)
	}
	return nil
}

// chunkedDownload streams a remote file to local in 16KB chunks via RunCommand
// + base64. Avoids the RunCommand stdout cap (~24KB) and the double-base64
// newline bug from issue #59.
func chunkedDownload(client *AliyunClient, instanceID, remotePath, localPath string) error {
	sizeRes, err := client.RunCommand(instanceID, fmt.Sprintf("stat -c%%s '%s'", remotePath), 15)
	if err != nil {
		return fmt.Errorf("stat remote: %w", err)
	}
	sizeStr := strings.TrimSpace(decodeOutput(sizeRes.Output))
	total, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return fmt.Errorf("parse remote size %q: %w", sizeStr, err)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local: %w", err)
	}
	defer f.Close()

	if total == 0 {
		return nil
	}

	nChunks := (total + chunkSize - 1) / chunkSize
	for i := int64(0); i < nChunks; i++ {
		// `dd skip=i bs=chunkSize count=1` reads the i-th block; pipe through
		// `base64 | tr -d '\n'` so the inner output has no newlines (issue #59
		// root cause was StdEncoding rejecting wrapped base64).
		cmd := fmt.Sprintf("dd if='%s' bs=%d count=1 skip=%d status=none 2>/dev/null | base64 | tr -d '\\n'", remotePath, chunkSize, i)
		res, err := client.RunCommand(instanceID, cmd, 60)
		if err != nil {
			return fmt.Errorf("download chunk %d/%d: %w", i+1, nChunks, err)
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(decodeOutput(res.Output)))
		if err != nil {
			return fmt.Errorf("decode chunk %d: %w", i+1, err)
		}
		if _, err := f.Write(decoded); err != nil {
			return fmt.Errorf("write local chunk %d: %w", i+1, err)
		}
		done := (i + 1) * int64(chunkSize)
		if done > total {
			done = total
		}
		fmt.Fprintf(os.Stderr, "\r📥 下载中 %d/%d (%d/%d B)", i+1, nChunks, done, total)
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

// shellQuote escapes a path for safe inclusion inside single quotes.
func shellQuote(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// ----- OSS-relay transfer -----
//
// For files in the hundreds-of-MB range, neither SendFile (≤24KB raw) nor
// chunked RunCommand can keep up. We hand the heavy lifting to OSS:
//
//   upload   local   → OSS (local ossutil) → presigned GET → curl on remote
//   download remote  → presigned PUT → curl on remote → OSS → local ossutil
//
// Remote side needs only `curl`. Local side needs `ossutil` already configured
// (the user has it). The bucket is passed via `--bucket NAME`.

// ossBucketEndpoint queries `ossutil stat` for a bucket's intranet endpoint.
// Falls back to extranet when intranet lookup fails. Intranet is preferred
// because the ECS curl call originates inside the Aliyun network.
func ossBucketEndpoint(bucket string) (intranet, extranet string, err error) {
	out, runErr := execCommand("ossutil", "stat", "oss://"+bucket).Output()
	if runErr != nil {
		return "", "", fmt.Errorf("ossutil stat oss://%s: %w", bucket, runErr)
	}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "IntranetEndpoint":
			intranet = v
		case "ExtranetEndpoint":
			extranet = v
		}
	}
	if intranet == "" && extranet == "" {
		return "", "", fmt.Errorf("could not parse endpoints for oss://%s", bucket)
	}
	if intranet == "" {
		intranet = extranet
	}
	return intranet, extranet, nil
}

// ossPresignV1 generates an OSS V1 query-auth URL for the given HTTP method.
// Content-MD5 and Content-Type are intentionally empty so that curl uploads
// without extra headers will match the signature.
func ossPresignV1(method, ak, sk, endpoint, bucket, object string, expiresUnix int64) string {
	stringToSign := fmt.Sprintf("%s\n\n\n%d\n/%s/%s", method, expiresUnix, bucket, object)
	mac := hmac.New(sha1.New, []byte(sk))
	mac.Write([]byte(stringToSign))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	// PathEscape keeps "/" but escapes spaces and special chars
	escapedObj := strings.ReplaceAll(url.PathEscape(object), "%2F", "/")
	return fmt.Sprintf("https://%s.%s/%s?OSSAccessKeyId=%s&Expires=%d&Signature=%s",
		bucket, endpoint, escapedObj,
		url.QueryEscape(ak), expiresUnix, url.QueryEscape(sig))
}

// doCopyViaOSS routes a tssh cp through an OSS bucket. Direction is inferred
// the same way as doCopy (which side has "name:path").
func doCopyViaOSS(src, dst, bucket string) {
	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")
	cache := getCache()
	ensureCache(cache)

	intranet, _, err := ossBucketEndpoint(bucket)
	fatal(err, "oss endpoint")

	if strings.Contains(dst, ":") {
		// Upload: local → OSS → remote
		name := dst[:strings.Index(dst, ":")]
		remotePath := dst[strings.Index(dst, ":")+1:]
		inst := resolveInstanceOrExit(cache, name)

		fileInfo, statErr := os.Stat(src)
		fatal(statErr, "stat local")

		key := fmt.Sprintf("tssh-cp/%d-%s", time.Now().UnixNano(), filepath.Base(src))
		ossURI := fmt.Sprintf("oss://%s/%s", bucket, key)
		fmt.Printf("⬆️  %s (%dMB) → OSS(%s) → %s:%s\n", src, fileInfo.Size()/1024/1024, bucket, inst.Name, remotePath)

		fmt.Println("📦 step 1/3: 本地 → OSS (ossutil)")
		up := execCommand("ossutil", "cp", "-f", src, ossURI)
		up.Stdout = os.Stdout
		up.Stderr = os.Stderr
		fatal(up.Run(), "ossutil cp upload")

		defer func() {
			rm := execCommand("ossutil", "rm", "-f", ossURI)
			rm.Run()
		}()

		signedGET := ossPresignV1("GET", config.AccessKeyID, config.AccessKeySecret, intranet, bucket, key, time.Now().Unix()+3600)
		fmt.Println("📦 step 2/3: 远端 curl ← OSS")
		// Build a target dir + curl pipeline. URL goes through env to avoid
		// shell-escaping headaches on weird query strings.
		remoteScript := fmt.Sprintf("mkdir -p '%s' && curl -fSL --retry 3 -o '%s' \"$TSSH_URL\"",
			shellQuote(filepath.Dir(remotePath)), shellQuote(remotePath))
		res, runErr := client.RunCommand(inst.ID, "TSSH_URL='"+strings.ReplaceAll(signedGET, "'", `'\''`)+"' bash -c "+singleQuote(remoteScript), 600)
		if runErr != nil {
			fatal(runErr, "remote curl")
		}
		if res.ExitCode != 0 {
			fmt.Fprintln(os.Stderr, decodeOutput(res.Output))
			fatal(fmt.Errorf("exit %d", res.ExitCode), "remote curl")
		}
		fmt.Println("📦 step 3/3: 校验大小")
		sizeRes, _ := client.RunCommand(inst.ID, fmt.Sprintf("stat -c%%s '%s'", shellQuote(remotePath)), 15)
		got := strings.TrimSpace(decodeOutput(sizeRes.Output))
		if want := strconv.FormatInt(fileInfo.Size(), 10); got != want {
			fatal(fmt.Errorf("size mismatch: local=%s remote=%s", want, got), "verify")
		}
		fmt.Println("✅ 完成")

	} else if strings.Contains(src, ":") {
		// Download: remote → OSS → local
		name := src[:strings.Index(src, ":")]
		remotePath := src[strings.Index(src, ":")+1:]
		inst := resolveInstanceOrExit(cache, name)

		key := fmt.Sprintf("tssh-cp/%d-%s", time.Now().UnixNano(), filepath.Base(remotePath))
		ossURI := fmt.Sprintf("oss://%s/%s", bucket, key)
		fmt.Printf("⬇️  %s:%s → OSS(%s) → %s\n", inst.Name, remotePath, bucket, dst)

		signedPUT := ossPresignV1("PUT", config.AccessKeyID, config.AccessKeySecret, intranet, bucket, key, time.Now().Unix()+3600)
		fmt.Println("📦 step 1/3: 远端 curl → OSS")
		remoteScript := fmt.Sprintf("curl -fSL --retry 3 -X PUT -T '%s' \"$TSSH_URL\"", shellQuote(remotePath))
		res, runErr := client.RunCommand(inst.ID, "TSSH_URL='"+strings.ReplaceAll(signedPUT, "'", `'\''`)+"' bash -c "+singleQuote(remoteScript), 600)
		if runErr != nil {
			fatal(runErr, "remote curl")
		}
		if res.ExitCode != 0 {
			fmt.Fprintln(os.Stderr, decodeOutput(res.Output))
			fatal(fmt.Errorf("exit %d", res.ExitCode), "remote curl")
		}

		defer func() {
			rm := execCommand("ossutil", "rm", "-f", ossURI)
			rm.Run()
		}()

		fmt.Println("📦 step 2/3: OSS → 本地 (ossutil)")
		dl := execCommand("ossutil", "cp", "-f", ossURI, dst)
		dl.Stdout = os.Stdout
		dl.Stderr = os.Stderr
		fatal(dl.Run(), "ossutil cp download")
		fmt.Println("📦 step 3/3: 完成")
		fmt.Println("✅ 完成")
	} else {
		fmt.Println("❌ 需要用 name:path 格式指定远程路径")
		os.Exit(1)
	}
}

// singleQuote wraps s in single quotes, escaping any embedded ones.
func singleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// cmdCopy copies files to/from instances. Flags:
//
//	-g/--grep <pat>   batch upload to all matching instances
//	--resume/-r       resume via portforward+rsync (requires ssh key)
//	--bucket <name>   relay through OSS bucket (best for ≥50MB files)
func cmdCopy(args []string) {
	grepMode := false
	resumeMode := false
	pattern := ""
	bucket := ""
	var remaining []string
	for i := 0; i < len(args); i++ {
		if (args[i] == "-g" || args[i] == "--grep") && i+1 < len(args) {
			grepMode = true
			pattern = args[i+1]
			i++
		} else if args[i] == "--resume" || args[i] == "-r" {
			resumeMode = true
		} else if args[i] == "--bucket" && i+1 < len(args) {
			bucket = args[i+1]
			i++
		} else {
			remaining = append(remaining, args[i])
		}
	}

	if len(remaining) < 2 {
		fmt.Println("用法: tssh cp <local> <name>:<remote>             (上传)")
		fmt.Println("      tssh cp <name>:<remote> <local>             (下载)")
		fmt.Println("      tssh cp -g <pattern> <local> :<remote>      (批量上传)")
		fmt.Println("      tssh cp --resume <local> <name>:<remote>    (断点续传)")
		fmt.Println("      tssh cp --bucket <oss-bucket> <src> <dst>   (走 OSS 中转, 适合数百 MB)")
		os.Exit(1)
	}

	if bucket != "" {
		doCopyViaOSS(remaining[0], remaining[1], bucket)
		return
	}
	if grepMode {
		doBatchCopy(pattern, remaining[0], remaining[1])
	} else if resumeMode {
		doResumeCopy(remaining[0], remaining[1])
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

	// Check file size for large file support — chunked uploads for anything
	// larger than a single SendFile call can hold.
	fileInfo, err := os.Stat(localPath)
	fatal(err, "stat file")
	largeFile := fileInfo.Size() > chunkSize

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
			var err error
			if largeFile {
				err = chunkedUpload(client, inst.ID, localPath, dir, fileName)
			} else {
				err = client.SendFile(inst.ID, localPath, dir, fileName)
			}
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

	inst := resolveInstanceOrExit(cache, name)

	localPort := findFreePort()
	fmt.Printf("🔗 %s → portforward :%d\n", inst.Name, localPort)
	stop, err := startPortForwardBgWithCancel(config, inst.ID, localPort, 21022)
	if err != nil {
		// Fallback to port 22
		stop, err = startPortForwardBgWithCancel(config, inst.ID, localPort, 22)
		fatal(err, "portforward")
	}
	defer stop()

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
	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")
	cache := getCache()
	ensureCache(cache)

	if strings.Contains(dst, ":") {
		// Upload
		name := dst[:strings.Index(dst, ":")]
		remotePath := dst[strings.Index(dst, ":")+1:]
		inst := resolveInstanceOrExit(cache, name)

		dir := filepath.Dir(remotePath)
		fileName := filepath.Base(src)
		if !strings.HasSuffix(remotePath, "/") && filepath.Base(remotePath) != "" {
			fileName = filepath.Base(remotePath)
			dir = filepath.Dir(remotePath)
		}

		// Check file size — chunk anything that exceeds Cloud Assistant's
		// SendFile limit (~24KB raw after base64 wrapping).
		fileInfo, statErr := os.Stat(src)
		if statErr == nil && fileInfo.Size() > chunkSize {
			fmt.Fprintf(os.Stderr, "⬆️  %s (%dKB) → %s:%s (分块上传)\n", src, fileInfo.Size()/1024, inst.Name, remotePath)
			err := chunkedUpload(client, inst.ID, src, dir, fileName)
			fatal(err, "chunked upload")
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
		inst := resolveInstanceOrExit(cache, name)

		fmt.Printf("⬇️  %s:%s → %s\n", inst.Name, remotePath, dst)
		err = chunkedDownload(client, inst.ID, remotePath, dst)
		fatal(err, "download")
		fmt.Println("✅ 完成")
	} else {
		fmt.Println("❌ 需要用 name:path 格式指定远程路径")
		os.Exit(1)
	}
}

// doResumeCopy uses rsync --partial for resumable file transfer
func doResumeCopy(src, dst string) {
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

	inst := resolveInstanceOrExit(cache, name)
	localPort := findFreePort()

	fmt.Printf("🔗 %s → portforward :%d (断点续传模式)\n", inst.Name, localPort)
	stop, err := startPortForwardBgWithCancel(config, inst.ID, localPort, 22)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	defer stop()

	homeDir, _ := os.UserHomeDir()
	sshKey := filepath.Join(homeDir, ".ssh", "id_rsa")
	sshOpts := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o PubkeyAcceptedAlgorithms=+ssh-rsa -o HostKeyAlgorithms=+ssh-rsa -o LogLevel=ERROR -i %s -p %d", sshKey, localPort)

	var rsyncArgs []string
	if upload {
		fmt.Printf("⬆️  rsync --partial %s → %s:%s\n", localPath, inst.Name, remotePath)
		rsyncArgs = []string{"-avz", "--partial", "--progress", "-e", sshOpts, localPath, fmt.Sprintf("root@127.0.0.1:%s", remotePath)}
	} else {
		fmt.Printf("⬇️  rsync --partial %s:%s → %s\n", inst.Name, remotePath, localPath)
		rsyncArgs = []string{"-avz", "--partial", "--progress", "-e", sshOpts, fmt.Sprintf("root@127.0.0.1:%s", remotePath), localPath}
	}

	rsync := execCommand("rsync", rsyncArgs...)
	rsync.Stdout = os.Stdout
	rsync.Stderr = os.Stderr
	rsync.Stdin = os.Stdin
	err = rsync.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ rsync failed: %v (可再次运行 --resume 继续)\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 完成")
}
