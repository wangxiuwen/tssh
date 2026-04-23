package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// asyncDefaultTimeoutSec is the server-side Cloud Assistant Timeout applied to
// --async submissions when the user did not explicitly pass --timeout.
// Async is used for long-running jobs (docker build, data migrations) — if we
// inherited the 60s default here, Cloud Assistant would kill the command long
// before the user fetches results. 24h is the Aliyun API max.
const asyncDefaultTimeoutSec = 86400

// cmdExecAsync submits the command to every target and prints the InvokeId
// for each. It does NOT wait for results — caller uses `tssh exec --fetch <id>`
// or `tssh exec --stop <id>` afterwards.
//
// This is the fix for the "command timed out but output lost" case: a long
// docker build / migration job can be submitted async and polled later.
func cmdExecAsync(client *AliyunClient, targets []Instance, command string, opts *execOptions) {
	type submission struct {
		Name       string `json:"name"`
		IP         string `json:"ip"`
		InstanceID string `json:"instance_id"`
		InvokeID   string `json:"invoke_id"`
		Error      string `json:"error,omitempty"`
	}

	timeoutSec := opts.timeout
	if !opts.timeoutSet {
		timeoutSec = asyncDefaultTimeoutSec
	}

	subs := make([]submission, 0, len(targets))
	for _, inst := range targets {
		s := submission{Name: inst.Name, IP: inst.PrivateIP, InstanceID: inst.ID}
		if inst.Status != "Running" {
			s.Error = "instance not running, skipped"
			subs = append(subs, s)
			continue
		}
		id, err := client.SubmitCommand(inst.ID, command, timeoutSec)
		if err != nil {
			s.Error = err.Error()
		} else {
			s.InvokeID = id
		}
		subs = append(subs, s)
	}

	if opts.jsonMode {
		data, _ := json.Marshal(subs)
		fmt.Println(string(data))
		return
	}

	if !opts.quietMode {
		fmt.Fprintf(os.Stderr, "🚀 async 提交 %d 个命令 (server timeout=%ds) — 用 `tssh exec --fetch <InvokeID>` 取结果\n\n", len(subs), timeoutSec)
	}
	for _, s := range subs {
		if s.Error != "" {
			fmt.Fprintf(os.Stderr, "❌ %s (%s): %s\n", s.Name, s.IP, s.Error)
			continue
		}
		fmt.Printf("%s\t%s\t%s\n", s.InvokeID, s.Name, s.IP)
	}
}

// cmdExecFetch fetches current state for a given InvokeId (one-shot, no polling).
func cmdExecFetch(opts *execOptions) {
	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	// Optional target — filters the API call to a single instance.
	instanceID := ""
	if len(opts.targets) > 0 {
		cache := getCache()
		ensureCache(cache)
		if inst, err := resolveInstance(cache, opts.targets[0]); err == nil {
			instanceID = inst.ID
		}
	}

	status, err := client.FetchInvocation(opts.fetchID, instanceID)
	fatal(err, "fetch invocation")

	output := decodeOutput(status.Output)

	if opts.jsonMode {
		// Return status with decoded output so downstream pipes work directly.
		type jsonStatus struct {
			*InvocationStatus
			DecodedOutput string `json:"decoded_output"`
		}
		data, _ := json.Marshal(jsonStatus{InvocationStatus: status, DecodedOutput: output})
		fmt.Println(string(data))
		return
	}

	if !opts.quietMode {
		fmt.Fprintf(os.Stderr, "━━━ %s [%s] on %s\n", status.InvokeID, status.Status, status.InstanceID)
		if status.StartTime != "" {
			fmt.Fprintf(os.Stderr, "   start:  %s\n", status.StartTime)
		}
		if status.FinishedTime != "" {
			fmt.Fprintf(os.Stderr, "   finish: %s\n", status.FinishedTime)
		}
		if status.ErrorCode != "" {
			fmt.Fprintf(os.Stderr, "   ❌ %s: %s\n", status.ErrorCode, status.ErrorInfo)
		}
		fmt.Fprintf(os.Stderr, "   exit:   %d\n", status.ExitCode)
		fmt.Fprintln(os.Stderr)
	}
	if output != "" {
		fmt.Print(output)
		if !strings.HasSuffix(output, "\n") {
			fmt.Println()
		}
	}

	// Terminal states carry a real exit code; Running/Pending leaves it 0.
	if isTerminalInvocation(status.Status) && status.ExitCode != 0 {
		os.Exit(status.ExitCode)
	}
}

// cmdExecStop cancels a running invocation by InvokeId.
func cmdExecStop(opts *execOptions) {
	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	var instanceIDs []string
	if len(opts.targets) > 0 {
		cache := getCache()
		ensureCache(cache)
		for _, name := range opts.targets {
			if inst, err := resolveInstance(cache, name); err == nil {
				instanceIDs = append(instanceIDs, inst.ID)
			}
		}
	}

	fatal(client.StopInvocation(opts.stopID, instanceIDs), "stop invocation")
	fmt.Fprintf(os.Stderr, "🛑 已停止 %s\n", opts.stopID)
}

func isTerminalInvocation(status string) bool {
	switch status {
	case "Success", "Finished", "Failed", "Stopped", "PartialFailed":
		return true
	}
	return false
}
