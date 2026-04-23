package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const version = "1.11.8"

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
	case "info":
		cmdInfo(filteredArgs[1:])
	case "tail":
		cmdTail(filteredArgs[1:])
	case "watch":
		cmdWatch(filteredArgs[1:])
	case "diff":
		cmdDiff(filteredArgs[1:])
	case "stop":
		cmdLifecycle("stop", filteredArgs[1:])
	case "start":
		cmdLifecycle("start", filteredArgs[1:])
	case "reboot":
		cmdLifecycle("reboot", filteredArgs[1:])
	case "top":
		cmdTop(filteredArgs[1:])
	case "tunnel":
		cmdTunnel(filteredArgs[1:])
	case "web":
		cmdWeb(filteredArgs[1:])
	case "redis":
		cmdRedis(filteredArgs[1:])
	case "rds":
		cmdRDS(filteredArgs[1:])
	case "arms":
		cmdArms(filteredArgs[1:])
	case "doctor":
		cmdDoctor()
	case "update":
		cmdUpdate()
	case "_portforward":
		// Internal: daemon mode for tunnel, args: <instanceID> <localPort> <remotePort>
		if len(filteredArgs) < 4 {
			os.Exit(1)
		}
		cfg := mustLoadConfig()
		lp, _ := strconv.Atoi(filteredArgs[2])
		rp, _ := strconv.Atoi(filteredArgs[3])
		if err := PortForward(cfg, filteredArgs[1], lp, rp); err != nil {
			fmt.Fprintf(os.Stderr, "portforward: %v\n", err)
			os.Exit(1)
		}
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
	case "-l", "--list":
		cmdList(filteredArgs[1:])
	default:
		// SSH-like: tssh [flags] <name> [command]
		target, localForward, command, timeout := parseSSHArgs(filteredArgs)

		if target != "" {
			if localForward != "" {
				cmdPortForward(target, localForward)
			} else if len(command) > 0 {
				cmdRemoteExec(target, strings.Join(command, " "), timeout)
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
func parseSSHArgs(args []string) (target, localForward string, command []string, timeout int) {
	timeout = 60
	if v := os.Getenv("TSSH_DEFAULT_TIMEOUT"); v != "" {
		if t, err := parseTimeoutSec(v); err == nil {
			timeout = t
		} else {
			fmt.Fprintf(os.Stderr, "⚠️ 忽略无效 TSSH_DEFAULT_TIMEOUT=%s: %v\n", v, err)
		}
	}
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
		case arg == "--":
			// "--" separates SSH options from remote command (POSIX convention)
			// Everything after "--" is the remote command
			if target == "" && i+1 < len(args) {
				target = args[i+1]
				command = append(command, args[i+2:]...)
			} else {
				command = append(command, args[i+1:]...)
			}
			return
		case arg == "-L":
			if i+1 < len(args) {
				localForward = args[i+1]
				i++
			}
		case arg == "--timeout":
			if i+1 < len(args) {
				t, err := parseTimeoutSec(args[i+1])
				if err != nil {
					fmt.Fprintf(os.Stderr, "❌ %v\n", err)
					os.Exit(2)
				}
				timeout = t
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
  tssh -L <port> <name>             端口转发 (简写: 同端口)
  tssh -L <local>:<host>:<remote> <name>   端口转发 (完整)

全局选项:
  --profile, -p <name>   使用指定账号配置

子命令:
  tssh ls [-j] [--tag k=v]         列出实例
  tssh sync                        同步实例缓存
  tssh exec [options] <target> <cmd>   远程执行
    --notify <url>                 执行后发送 webhook 通知
    --timeout <sec|duration>       整数秒或 duration 如 5m/2h (默认: $TSSH_DEFAULT_TIMEOUT 或 60)
  tssh cp [-g <pat>] <src> <dst>   文件拷贝
  tssh health [-g <pat>]           健康检查
  tssh ping [-g <pat>] [<name>]    连通性测试
  tssh info <name>                 实例详情
  tssh tail <name> <path>          远程日志跟踪
  tssh watch [-g <pat>] <cmd>      定时轮询执行
  tssh diff -g <pat> <cmd>         多机输出对比
  tssh stop/start/reboot <name>    实例生命周期
  tssh top [-g <pat>]              实时监控面板
  tssh tunnel start/list/stop      持久化隧道管理
  tssh web [--port <port>] [--token <tok>] [--bind 0.0.0.0]  Web 管理面板
                                   默认只绑 127.0.0.1; --bind 非本地时必须有 --token
  tssh redis ls [-j]               列出 Redis 实例
  tssh redis info <name|id> [-j]   Redis 实例详情
  tssh redis <name|id>             连接 Redis (自动端口转发)
  tssh rds ls [-j]                 列出 RDS 实例
  tssh rds info <name|id> [-j]     RDS 实例详情
  tssh rds <name|id>               连接 RDS (自动端口转发)
  tssh arms                        查看 ARMS 触发中的告警
  tssh arms alerts [-j]            告警详情
  tssh arms dash [keyword] [-j]    列出/搜索仪表盘
  tssh arms ds [-j]                列出数据源
  tssh arms open [keyword]         浏览器打开仪表盘
  tssh arms query <promql|shortcut> Prometheus 查询
  tssh arms trace <TraceID>        查看 trace 完整 span 列表
  tssh arms trace --globalId <v>   按 globalId 搜索 trace
  tssh arms trace --tag k=v        按自定义 tag 搜索 trace
  tssh doctor                      自检
  tssh update                      自更新
  tssh ssh-config                  生成 SSH config
  tssh profiles                    列出所有账号
  tssh history                     查看执行历史

exec 选项:
  -g <keyword>     批量执行 (支持正则/多关键字/tag:key=val)
  -j, --json       JSON 输出
  -q, --quiet      安静模式
  -s, --script <f> 从文件执行
  --timeout <sec|duration>  超时, 整数秒或 Go duration (60 / 5m / 2h); 默认 60s
  --progress       显示进度
  --tag <k=v>      按标签过滤
  -                从 stdin 读取

exec 异步/恢复 (长任务推荐):
  --async          提交后立即返回 InvokeId, 不等待结果
  --fetch <id>     按 InvokeId 单次拉取输出 (Running 也能看到部分输出)
  --stop <id>      按 InvokeId 强停远程命令

端口转发简写:
  tssh -L 3306 myhost              等价于 -L 3306:localhost:3306
  tssh -L 3306:dbhost myhost       等价于 -L 3306:dbhost:3306

配套工具:
  tscp / trsync`)
}

// cmdConnect connects interactively

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

func resolveInstance(c *Cache, name string) (*Instance, error) {
	ensureCache(c)
	instances, err := c.Load()
	if err != nil {
		return nil, fmt.Errorf("加载缓存失败: %w", err)
	}

	if name == "" {
		inst, err := FuzzySelect(instances, "")
		if err != nil {
			return nil, fmt.Errorf("cancelled")
		}
		return inst, nil
	}

	if idx, err := strconv.Atoi(name); err == nil && idx >= 1 && idx <= len(instances) {
		return &instances[idx-1], nil
	}

	inst, err := c.FindByName(name)
	if err == nil {
		return inst, nil
	}

	matches, _ := c.FindByPattern(name)
	if len(matches) == 1 {
		return &matches[0], nil
	}

	if len(matches) > 1 {
		inst, err := FuzzySelect(instances, name)
		if err != nil {
			return nil, fmt.Errorf("cancelled")
		}
		return inst, nil
	}

	return nil, fmt.Errorf("找不到 '%s'", name)
}

// resolveInstanceOrExit wraps resolveInstance for CLI callers that need os.Exit behavior
func resolveInstanceOrExit(c *Cache, name string) *Instance {
	inst, err := resolveInstance(c, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	return inst
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
