package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// cmdBrowser opens a Chrome/Chromium/Edge window whose *every* connection
// goes through a fresh SOCKS5 proxy pointing at the chosen ECS. Main trick:
//   --user-data-dir: isolates profile / cookies / extensions from the user's
//     day-to-day browser, so no accidental proxying of gmail etc.
//   --proxy-server="socks5://...": Chrome honours this flag natively.
//   --proxy-bypass-list: keep localhost out of the proxy.
//
// This kills the "I need kubectl port-forward 8 things just to click around
// the internal Grafana" pattern.
//
//	tssh browser prod-jump
//	tssh browser prod-jump http://grafana.internal
//	tssh browser prod-jump --chrome /Applications/Brave\ Browser.app/Contents/MacOS/Brave\ Browser
func cmdBrowser(args []string) {
	localPort := 0 // auto by default; avoids colliding with a running `tssh socks`
	remotePort := 19080
	chromePath := ""
	var target string
	var urls []string
	var jsonMode bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-j", "--json":
			jsonMode = true
		case "-p", "--port":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "❌ -p 需要端口号")
				os.Exit(2)
			}
			v, err := strconv.Atoi(args[i+1])
			if err != nil || v <= 0 || v > 65535 {
				fmt.Fprintf(os.Stderr, "❌ 无效端口: %s\n", args[i+1])
				os.Exit(2)
			}
			localPort = v
			i++
		case "--chrome", "--browser":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "❌ --chrome 需要一个可执行路径")
				os.Exit(2)
			}
			chromePath = args[i+1]
			i++
		case "-h", "--help":
			printBrowserHelp()
			return
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "❌ 未知选项: %s\n", args[i])
				os.Exit(2)
			}
			if target == "" {
				target = args[i]
			} else {
				// Subsequent positional args are URLs to open.
				urls = append(urls, args[i])
			}
		}
	}

	if target == "" {
		printBrowserHelp()
		os.Exit(1)
	}

	if chromePath == "" {
		chromePath = findChromeLike()
		if chromePath == "" {
			fatalMsg(`未找到 Chrome / Chromium / Edge. 用 --chrome <path> 指定:
  macOS:   --chrome '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
  Linux:   --chrome /usr/bin/google-chrome  (或 chromium / microsoft-edge)
  Windows: tssh browser 暂不支持, 请手动启动 Chrome 加 --proxy-server 参数`)
		}
	}

	cache := getCache()
	ensureCache(cache)
	inst := resolveInstanceOrExit(cache, target)

	cfg := mustLoadConfig()
	client, err := NewAliyunClient(cfg)
	fatal(err, "create client")

	if !jsonMode {
		fmt.Fprintf(os.Stderr, "🔌 在 %s 上启动 SOCKS5 (microsocks)...\n", inst.Name)
	}
	pid, err := startRemoteSocks(client, inst.ID, remotePort)
	fatal(err, "start microsocks")
	cleanupSocks := func() {
		_, _ = client.RunCommand(inst.ID, fmt.Sprintf("kill %s 2>/dev/null", shellQuote(pid)), 5)
	}
	defer cleanupSocks()

	if localPort == 0 {
		localPort = findFreePort()
	}
	stop, err := startPortForwardBgWithCancel(cfg, inst.ID, localPort, remotePort)
	if err != nil {
		cleanupSocks()
		fatal(err, "portforward")
	}
	defer stop()

	// Dedicated user-data-dir per-invocation — avoids ever touching the
	// user's real profile. Lives under tmpdir + timestamp so repeated runs
	// don't clash.
	profileDir := filepath.Join(os.TempDir(), fmt.Sprintf("tssh-browser-%s-%d", safeFilename(inst.Name), time.Now().Unix()))
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		cleanupSocks()
		stop()
		fatal(err, "create profile dir")
	}

	chromeArgs := []string{
		"--user-data-dir=" + profileDir,
		fmt.Sprintf("--proxy-server=socks5://127.0.0.1:%d", localPort),
		"--proxy-bypass-list=<-loopback>",
		"--no-first-run",
		"--no-default-browser-check",
		// Keep every invocation as its own window / process tree so the
		// cleanup kill below doesn't kill the user's other Chrome windows.
		"--new-window",
	}
	chromeArgs = append(chromeArgs, urls...)

	c := exec.Command(chromePath, chromeArgs...)
	// Stdout/stderr of Chrome is noisy; only surface it in human mode.
	if !jsonMode {
		c.Stdout = os.Stderr
		c.Stderr = os.Stderr
	}
	if err := c.Start(); err != nil {
		cleanupSocks()
		stop()
		fatal(err, "start chrome")
	}

	cleanupChrome := func() {
		if c.Process != nil {
			_ = c.Process.Signal(syscall.SIGTERM)
			done := make(chan struct{})
			go func() { _ = c.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = c.Process.Kill()
			}
		}
		// Profile dir has cookies / cache; clean it up so the user isn't left
		// with pile of temp dirs over time.
		_ = os.RemoveAll(profileDir)
	}
	defer cleanupChrome()

	if jsonMode {
		payload := map[string]interface{}{
			"local_port":    localPort,
			"proxy":         fmt.Sprintf("socks5://127.0.0.1:%d", localPort),
			"via":           inst.Name,
			"jump_id":       inst.ID,
			"chrome_pid":    c.Process.Pid,
			"profile_dir":   profileDir,
			"chrome_path":   chromePath,
			"opened_urls":   urls,
			"pid":           os.Getpid(),
		}
		b, _ := json.Marshal(payload)
		fmt.Println(string(b))
		os.Stdout.Sync()
	} else {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "🌐 浏览器已打开 — 所有请求走 %s (SOCKS5 127.0.0.1:%d)\n", inst.Name, localPort)
		fmt.Fprintf(os.Stderr, "   profile dir: %s  (tssh 退出时会删)\n", profileDir)
		fmt.Fprintln(os.Stderr, "   按 Ctrl+C 关浏览器并清理.")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Exit when EITHER the user presses Ctrl-C OR the browser window is closed.
	// The latter is what people actually do — click the close button, tssh
	// should notice and clean up instead of leaving microsocks hanging.
	chromeDone := make(chan struct{})
	go func() {
		_ = c.Wait()
		close(chromeDone)
	}()
	select {
	case <-sigCh:
	case <-chromeDone:
		if !jsonMode {
			fmt.Fprintln(os.Stderr, "\n🛑 浏览器已关, 清理 SOCKS...")
		}
	}
}

// findChromeLike walks the common install paths for Chromium-based browsers
// and returns the first one it finds executable. Order picked to prefer the
// user's likely daily driver.
func findChromeLike() string {
	switch runtime.GOOS {
	case "darwin":
		for _, p := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Arc.app/Contents/MacOS/Arc",
		} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	case "linux":
		for _, p := range []string{
			"google-chrome-stable", "google-chrome", "chromium", "chromium-browser", "microsoft-edge", "brave-browser",
		} {
			if full, err := exec.LookPath(p); err == nil {
				return full
			}
		}
	}
	return ""
}

// safeFilename strips characters that would be awkward or unsafe inside a
// path component. Only used for cosmetic labeling of the temp profile dir.
func safeFilename(s string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return replacer.Replace(s)
}

func printBrowserHelp() {
	fmt.Println(`用法: tssh browser <name> [url ...] [-p <port>] [--chrome <path>] [-j]

打开一个带独立 profile 的 Chrome/Chromium/Edge 窗口, 所有请求走远端 ECS
的 SOCKS5 代理. 取代 "kubectl port-forward 一堆端口然后本地浏览器访问"
的笨办法.

效果等于 "在那台 ECS 上打开浏览器":
  - 窗口里访问 Kubernetes Dashboard / Grafana / 内网 CMDB / 任何内网 Web
  - 和主浏览器完全隔离 (独立 --user-data-dir, 不动日常 cookies/extensions)
  - 关掉窗口或 Ctrl+C → tssh 自动清 SOCKS / 远端 microsocks / profile dir

选项:
  [url ...]               启动时直接打开的页面 (可多个)
  -p, --port <port>       本地 SOCKS5 端口 (默认: 自动分配空闲端口)
  --chrome, --browser <p> Chrome/Chromium/Edge 可执行路径 (默认自动探测)
  -j, --json              浏览器开启后 stdout 一行 JSON (AI/脚本用)

自动探测顺序:
  macOS:  Google Chrome → Chromium → Edge → Brave → Arc
  Linux:  google-chrome → chromium → microsoft-edge → brave

示例:
  tssh browser prod-jump
  tssh browser prod-jump http://grafana.internal http://dashboard.internal
  tssh browser prod-jump --chrome /Applications/Brave\ Browser.app/Contents/MacOS/Brave\ Browser

JSON 输出:
  {"local_port":54321,"proxy":"socks5://127.0.0.1:54321","via":"prod-jump",
   "jump_id":"i-...","chrome_pid":1234,"profile_dir":"/tmp/tssh-browser-...",
   "chrome_path":"...","opened_urls":[],"pid":5678}

注意:
  - 隔离 profile 意味着浏览器里的登录状态/书签不会同步到主浏览器, 这正是我们要的
  - 如果内网站点要 Kubernetes dashboard 的 kubeconfig cookie,
    那在这个独立 profile 里再登一次即可`)
}
