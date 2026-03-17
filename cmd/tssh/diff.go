package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// cmdDiff runs the same command on multiple instances and highlights differences
func cmdDiff(args []string) {
	pattern := ""
	var command []string

	i := 0
	for i < len(args) {
		switch args[i] {
		case "-g", "--grep":
			if i+1 < len(args) {
				pattern = args[i+1]
				i += 2
			}
		default:
			command = args[i:]
			i = len(args)
		}
	}

	if pattern == "" || len(command) == 0 {
		fmt.Fprintln(os.Stderr, "用法: tssh diff -g <pattern> <command>")
		os.Exit(1)
	}

	cmdStr := strings.Join(command, " ")

	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	cache := getCache()
	ensureCache(cache)

	instances, _ := cache.FindByPattern(pattern)
	if len(instances) < 2 {
		fmt.Fprintln(os.Stderr, "❌ 至少需要 2 台实例才能做对比")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "🔍 对比 %d 台机器: %s\n\n", len(instances), cmdStr)

	type diffResult struct {
		Name   string
		Output string
		Err    error
	}

	results := make([]diffResult, len(instances))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for i, inst := range instances {
		if inst.Status != "Running" {
			results[i] = diffResult{Name: inst.Name, Err: fmt.Errorf("not running")}
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, inst Instance) {
			defer wg.Done()
			defer func() { <-sem }()
			result, err := client.RunCommand(inst.ID, cmdStr, 30)
			r := diffResult{Name: inst.Name}
			if err != nil {
				r.Err = err
			} else {
				r.Output = strings.TrimRight(decodeOutput(result.Output), "\n")
			}
			results[idx] = r
		}(i, inst)
	}
	wg.Wait()

	// Use first successful result as baseline
	baseline := ""
	baselineName := ""
	for _, r := range results {
		if r.Err == nil {
			baseline = r.Output
			baselineName = r.Name
			break
		}
	}

	allSame := true
	for _, r := range results {
		if r.Err == nil && r.Output != baseline {
			allSame = false
			break
		}
	}

	if allSame {
		fmt.Printf("✅ 所有 %d 台机器输出相同\n\n", len(instances))
		fmt.Println(baseline)
		return
	}

	// Show differences
	baselineLines := strings.Split(baseline, "\n")

	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("❌ [%s] %v\n\n", r.Name, r.Err)
			continue
		}

		if r.Output == baseline {
			fmt.Printf("✅ [%s] 与 [%s] 相同\n", r.Name, baselineName)
			continue
		}

		fmt.Printf("⚠️  [%s] 与 [%s] 不同:\n", r.Name, baselineName)
		diffLines := strings.Split(r.Output, "\n")

		maxLines := len(baselineLines)
		if len(diffLines) > maxLines {
			maxLines = len(diffLines)
		}

		for li := 0; li < maxLines; li++ {
			baseLine := ""
			diffLine := ""
			if li < len(baselineLines) {
				baseLine = baselineLines[li]
			}
			if li < len(diffLines) {
				diffLine = diffLines[li]
			}

			if baseLine != diffLine {
				if baseLine != "" {
					fmt.Printf("  \033[31m- %s\033[0m\n", baseLine)
				}
				if diffLine != "" {
					fmt.Printf("  \033[32m+ %s\033[0m\n", diffLine)
				}
			}
		}
		fmt.Println()
	}
}
