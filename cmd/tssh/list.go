package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// cmdList prints all cached instances
func cmdList(args []string) {
	jsonMode := false
	tagFilter := ""
	searchPattern := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-j", "--json":
			jsonMode = true
		case "--tag":
			if i+1 < len(args) {
				tagFilter = args[i+1]
				i++
			}
		default:
			searchPattern = args[i]
		}
	}

	cache := getCache()
	ensureCache(cache)
	instances, err := cache.Load()
	fatal(err, "load cache")

	// Apply tag filter
	if tagFilter != "" {
		instances = FilterInstances(instances, "tag:"+tagFilter)
	}
	// Apply search pattern
	if searchPattern != "" {
		instances = FilterInstances(instances, searchPattern)
	}

	if jsonMode {
		data, _ := json.Marshal(instances)
		fmt.Println(string(data))
	} else {
		PrintInstanceList(instances)
	}
}

// cmdSync fetches all instances from Aliyun API
func cmdSync() {
	config := mustLoadConfig()
	client, err := NewAliyunClient(config)
	fatal(err, "create client")

	fmt.Fprintf(os.Stderr, "🔄 正在从阿里云拉取 ECS 实例列表 (profile: %s, region: %s)...\n", config.ProfileName, config.Region)
	instances, err := client.FetchAllInstances()
	fatal(err, "fetch instances")

	cache := getCache()
	err = cache.Save(instances)
	fatal(err, "save cache")

	fmt.Fprintf(os.Stderr, "✅ 缓存已保存 (%d 台实例)\n", len(instances))
}

// cmdSyncQuiet fetches instances without printing progress (for auto-sync).
// Returns error instead of os.Exit so callers running in a long-lived process
// (tssh web, auto-refresh in main) don't die on a transient API failure.
func cmdSyncQuiet() error {
	// Honor globalProfile — LoadConfig() with empty profile picks the default
	// one from config.json which may be the wrong account.
	config, err := LoadConfigWithProfile(globalProfile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	client, err := NewAliyunClient(config)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	instances, err := client.FetchAllInstances()
	if err != nil {
		return fmt.Errorf("fetch instances: %w", err)
	}
	cache := getCache()
	if err := cache.Save(instances); err != nil {
		return fmt.Errorf("save cache: %w", err)
	}
	return nil
}

// execOptions holds parsed flags for the exec command
type execOptions struct {
	grepMode   bool
	jsonMode   bool
	quietMode  bool
	progress   bool
	timeout    int
	scriptFile string
	stdinMode  bool
	tagFilter  string
	notifyURL  string
	pattern    string
	targets    []string
	command    string
	asyncMode  bool   // submit only, print InvokeId, exit 0
	fetchID    string // one-shot DescribeInvocationResults
	stopID     string // cancel a running invocation
	timeoutSet bool   // true if user explicitly passed --timeout / env var
}
