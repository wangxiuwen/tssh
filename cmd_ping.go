package main

import (
"fmt"
"os"
"sync"
"time"
)

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
