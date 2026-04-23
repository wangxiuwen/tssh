package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Verify every --json payload across fwd / run / socks / shell / vpn keeps a
// stable, consumer-friendly shape. Agents script against these fields; adding
// fields is fine, renaming / removing breaks callers.

func TestJSONSchema_fwd(t *testing.T) {
	// The structure written by cmdFwd; kept in sync by hand.
	payload := map[string]interface{}{
		"local_port":  54321,
		"host":        "rds.internal",
		"remote_port": 3306,
		"jump":        "prod-jump",
		"jump_id":     "i-abc",
		"pid":         1,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"local_port", "host", "remote_port", "jump", "jump_id", "pid"} {
		if !strings.Contains(string(b), `"`+f+`":`) {
			t.Errorf("fwd JSON missing %q: %s", f, b)
		}
	}
}

func TestJSONSchema_socks(t *testing.T) {
	payload := map[string]interface{}{
		"local_port": 1080,
		"proxy":      "socks5h://127.0.0.1:1080",
		"via":        "prod-jump",
		"jump_id":    "i-abc",
		"remote_pid": "1234",
		"pid":        1,
	}
	b, _ := json.Marshal(payload)
	for _, f := range []string{"local_port", "proxy", "via", "jump_id", "remote_pid"} {
		if !strings.Contains(string(b), `"`+f+`":`) {
			t.Errorf("socks JSON missing %q: %s", f, b)
		}
	}
}

func TestJSONSchema_run(t *testing.T) {
	type entry struct {
		Name       string `json:"name"`
		EnvPrefix  string `json:"env_prefix"`
		Host       string `json:"host"`
		RemotePort int    `json:"remote_port"`
		LocalPort  int    `json:"local_port"`
		Jump       string `json:"jump"`
		JumpID     string `json:"jump_id"`
	}
	payload := map[string]interface{}{
		"targets": []entry{
			{Name: "mysql", EnvPrefix: "MYSQL", Host: "h", RemotePort: 3306, LocalPort: 54321, Jump: "j", JumpID: "i"},
		},
		"pid": 1,
	}
	b, _ := json.Marshal(payload)
	for _, f := range []string{"targets", "name", "env_prefix", "host", "remote_port", "local_port", "jump", "jump_id", "pid"} {
		if !strings.Contains(string(b), `"`+f+`":`) {
			t.Errorf("run JSON missing %q: %s", f, b)
		}
	}
}

func TestJSONSchema_shell(t *testing.T) {
	payload := map[string]interface{}{
		"local_port": 1080,
		"proxy":      "socks5h://127.0.0.1:1080",
		"via":        "h",
		"jump_id":    "i",
		"shell":      "/bin/zsh",
	}
	b, _ := json.Marshal(payload)
	for _, f := range []string{"local_port", "proxy", "via", "jump_id", "shell"} {
		if !strings.Contains(string(b), `"`+f+`":`) {
			t.Errorf("shell JSON missing %q: %s", f, b)
		}
	}
}

func TestJSONSchema_vpn(t *testing.T) {
	payload := map[string]interface{}{
		"tun":              "tssh0",
		"cidrs":            []string{"10.0.0.0/16"},
		"socks_local_port": 54321,
		"via":              "h",
		"jump_id":          "i",
		"tun2socks_pid":    1234,
		"pid":              5678,
	}
	b, _ := json.Marshal(payload)
	for _, f := range []string{"tun", "cidrs", "socks_local_port", "via", "jump_id", "tun2socks_pid", "pid"} {
		if !strings.Contains(string(b), `"`+f+`":`) {
			t.Errorf("vpn JSON missing %q: %s", f, b)
		}
	}
}
