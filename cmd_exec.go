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
)


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
