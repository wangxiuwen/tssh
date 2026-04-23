package main

import (
	"strings"
	"testing"
)

// Find a KEY=... entry in the env slice; returns value or empty.
func envVal(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
}

// Count how many entries share a key so we catch accidental duplication.
func envCount(env []string, key string) int {
	prefix := key + "="
	n := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			n++
		}
	}
	return n
}

func TestBuildShellEnv_AllProxyVarsSet(t *testing.T) {
	out := buildShellEnv([]string{"PATH=/usr/bin"}, 1080, "prod-jump", "bash")

	for _, key := range []string{"ALL_PROXY", "all_proxy", "HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"} {
		v := envVal(out, key)
		if v != "socks5h://127.0.0.1:1080" {
			t.Errorf("%s = %q, want socks5h://127.0.0.1:1080", key, v)
		}
	}
	if envVal(out, "TSSH_SHELL_HOST") != "prod-jump" {
		t.Errorf("TSSH_SHELL_HOST: %v", envVal(out, "TSSH_SHELL_HOST"))
	}
}

func TestBuildShellEnv_NoProxyIncludesLocalhost(t *testing.T) {
	out := buildShellEnv(nil, 1080, "h", "bash")
	v := envVal(out, "NO_PROXY")
	if !strings.Contains(v, "127.0.0.1") || !strings.Contains(v, "localhost") {
		t.Errorf("NO_PROXY %q missing localhost entries", v)
	}
}

func TestBuildShellEnv_JVMOptionsAppended(t *testing.T) {
	// Parent's JAVA_TOOL_OPTIONS should be preserved, ours appended.
	parent := []string{"JAVA_TOOL_OPTIONS=-XshowSettings:vm"}
	out := buildShellEnv(parent, 1080, "h", "bash")
	v := envVal(out, "JAVA_TOOL_OPTIONS")
	if !strings.Contains(v, "-XshowSettings:vm") {
		t.Errorf("lost parent JVM options: %q", v)
	}
	if !strings.Contains(v, "-DsocksProxyHost=127.0.0.1") {
		t.Errorf("missing socks host flag: %q", v)
	}
	if !strings.Contains(v, "-DsocksProxyPort=1080") {
		t.Errorf("missing socks port flag: %q", v)
	}
	if envCount(out, "JAVA_TOOL_OPTIONS") != 1 {
		t.Errorf("JAVA_TOOL_OPTIONS duplicated: %d", envCount(out, "JAVA_TOOL_OPTIONS"))
	}
}

func TestBuildShellEnv_ExistingProxyReplaced(t *testing.T) {
	// If parent had ALL_PROXY set (e.g. nested tssh shell), child should get
	// OUR value, not the stale one. One value per var, not two.
	parent := []string{"ALL_PROXY=socks5://127.0.0.1:9999", "PATH=/usr/bin"}
	out := buildShellEnv(parent, 1080, "h", "bash")
	if envCount(out, "ALL_PROXY") != 1 {
		t.Errorf("ALL_PROXY should appear once, got %d", envCount(out, "ALL_PROXY"))
	}
	if envVal(out, "ALL_PROXY") != "socks5h://127.0.0.1:1080" {
		t.Errorf("stale ALL_PROXY leaked: %q", envVal(out, "ALL_PROXY"))
	}
	// PATH must be preserved.
	if envVal(out, "PATH") != "/usr/bin" {
		t.Errorf("PATH lost: %q", envVal(out, "PATH"))
	}
}

func TestShellHelp_NoPanic(t *testing.T) {
	printShellHelp()
}
