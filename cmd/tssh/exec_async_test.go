package main

import "testing"

func TestParseExecArgs_Async(t *testing.T) {
	opts := parseExecArgs([]string{"--async", "host-1", "sleep", "9999"})
	if !opts.asyncMode {
		t.Error("expected asyncMode=true")
	}
	if len(opts.targets) != 3 || opts.targets[0] != "host-1" {
		t.Errorf("targets=%v", opts.targets)
	}
}

func TestParseExecArgs_Fetch(t *testing.T) {
	opts := parseExecArgs([]string{"--fetch", "inv-123"})
	if opts.fetchID != "inv-123" {
		t.Errorf("fetchID=%s", opts.fetchID)
	}
	if opts.asyncMode || opts.stopID != "" {
		t.Error("should not mix modes")
	}
}

func TestParseExecArgs_Stop(t *testing.T) {
	opts := parseExecArgs([]string{"--stop", "inv-9", "host-1"})
	if opts.stopID != "inv-9" {
		t.Errorf("stopID=%s", opts.stopID)
	}
	if len(opts.targets) != 1 || opts.targets[0] != "host-1" {
		t.Errorf("targets=%v", opts.targets)
	}
}

func TestIsTerminalInvocation(t *testing.T) {
	cases := map[string]bool{
		"Success":       true,
		"Finished":      true,
		"Failed":        true,
		"Stopped":       true,
		"PartialFailed": true,
		"Running":       false,
		"Pending":       false,
		"":              false,
	}
	for status, want := range cases {
		if got := isTerminalInvocation(status); got != want {
			t.Errorf("isTerminalInvocation(%q) = %v, want %v", status, got, want)
		}
	}
}
