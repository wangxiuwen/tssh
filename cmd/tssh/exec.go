package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// parseTimeoutSec accepts a bare integer ("300") OR a Go duration ("5m", "2h30m").
// Returns seconds and a parse error; never silently falls through to zero,
// because that used to collapse local poll deadlines to ~10s and made users
// think --timeout was ignored.
func parseTimeoutSec(s string) (int, error) {
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("timeout 必须大于 0: %s", s)
		}
		return n, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("无法解析 timeout: %q (期望整数秒或 Go duration, 如 300 / 5m / 2h)", s)
	}
	sec := int(d.Seconds())
	if sec <= 0 {
		return 0, fmt.Errorf("timeout 必须大于 0: %s", s)
	}
	return sec, nil
}

func parseExecArgs(args []string) *execOptions {
	defaultTimeout := 60
	timeoutSet := false
	if v := os.Getenv("TSSH_DEFAULT_TIMEOUT"); v != "" {
		if t, err := parseTimeoutSec(v); err == nil {
			defaultTimeout = t
			timeoutSet = true
		} else {
			fmt.Fprintf(os.Stderr, "⚠️ 忽略无效 TSSH_DEFAULT_TIMEOUT=%s: %v\n", v, err)
		}
	}
	opts := &execOptions{timeout: defaultTimeout, timeoutSet: timeoutSet}
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
				t, err := parseTimeoutSec(args[i+1])
				if err != nil {
					fmt.Fprintf(os.Stderr, "❌ %v\n", err)
					os.Exit(2)
				}
				opts.timeout = t
				opts.timeoutSet = true
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
		case "--notify":
			if i+1 < len(args) {
				opts.notifyURL = args[i+1]
				i++
			}
		case "--async":
			opts.asyncMode = true
		case "--fetch":
			if i+1 < len(args) {
				opts.fetchID = args[i+1]
				i++
			}
		case "--stop":
			if i+1 < len(args) {
				opts.stopID = args[i+1]
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

	// Invocation management modes — operate on InvokeId, not instance name.
	if opts.fetchID != "" {
		cmdExecFetch(opts)
		return
	}
	if opts.stopID != "" {
		cmdExecStop(opts)
		return
	}

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
		inst := resolveInstanceOrExit(cache, targetName)
		targets = []Instance{*inst}
	}

	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "❌ 没有匹配的实例")
		os.Exit(1)
	}

	// Async mode: submit and exit, so long-running commands (docker build, etc.)
	// aren't lost on local timeout. User retrieves output via `tssh exec --fetch`.
	if opts.asyncMode {
		cmdExecAsync(client, targets, command, opts)
		return
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
		InvokeID string `json:"invoke_id,omitempty"` // set on timeout — use with --fetch
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
				// Timeout carries InvokeID so the user (or downstream tooling) can
				// retrieve output later via `tssh exec --fetch <id>`.
				if te, ok := err.(*TimeoutError); ok {
					r.InvokeID = te.InvokeID
				}
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
			if opts.notifyURL != "" {
				sendWebhook(opts.notifyURL, fmt.Sprintf("tssh exec: %d targets, max exit %d", len(targets), maxExitCode), command)
			}
			os.Exit(maxExitCode)
		}
	}

	// Send webhook notification if configured
	if opts.notifyURL != "" {
		var summary string
		for _, r := range results {
			if r.Output != "" {
				summary += fmt.Sprintf("[%s] exit:%d\n%s\n", r.Name, r.ExitCode, truncateStr(r.Output, 200))
			}
		}
		sendWebhook(opts.notifyURL, fmt.Sprintf("tssh exec: %d targets completed", len(targets)), summary)
	}
}
