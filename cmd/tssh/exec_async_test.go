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

func TestParseTimeoutSec_Integer(t *testing.T) {
	cases := map[string]int{
		"60":   60,
		"300":  300,
		"3600": 3600,
	}
	for in, want := range cases {
		got, err := parseTimeoutSec(in)
		if err != nil {
			t.Errorf("parseTimeoutSec(%q) unexpected error: %v", in, err)
		}
		if got != want {
			t.Errorf("parseTimeoutSec(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseTimeoutSec_Duration(t *testing.T) {
	cases := map[string]int{
		"5m":     300,
		"2h":     7200,
		"1h30m":  5400,
		"500ms":  0, // rounds down to 0 → error
		"300s":   300,
	}
	for in, want := range cases {
		got, err := parseTimeoutSec(in)
		if want == 0 {
			if err == nil {
				t.Errorf("parseTimeoutSec(%q) should have errored (rounded to 0)", in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseTimeoutSec(%q) unexpected error: %v", in, err)
		}
		if got != want {
			t.Errorf("parseTimeoutSec(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseTimeoutSec_Invalid(t *testing.T) {
	for _, bad := range []string{"", "abc", "-5", "0", "5x"} {
		if _, err := parseTimeoutSec(bad); err == nil {
			t.Errorf("parseTimeoutSec(%q) should have errored", bad)
		}
	}
}

func TestParseExecArgs_TimeoutDuration(t *testing.T) {
	opts := parseExecArgs([]string{"--timeout", "5m", "host-1", "cmd"})
	if opts.timeout != 300 {
		t.Errorf("expected 300, got %d", opts.timeout)
	}
	if !opts.timeoutSet {
		t.Error("timeoutSet should be true after explicit --timeout")
	}
}

func TestParseExecArgs_TimeoutNotSet(t *testing.T) {
	t.Setenv("TSSH_DEFAULT_TIMEOUT", "") // clear potential env leak
	opts := parseExecArgs([]string{"host-1", "cmd"})
	if opts.timeout != 60 {
		t.Errorf("expected default 60, got %d", opts.timeout)
	}
	if opts.timeoutSet {
		t.Error("timeoutSet should be false without --timeout / env var")
	}
}

func TestParseExecArgs_TimeoutFromEnv(t *testing.T) {
	t.Setenv("TSSH_DEFAULT_TIMEOUT", "10m")
	opts := parseExecArgs([]string{"host-1", "cmd"})
	if opts.timeout != 600 {
		t.Errorf("expected 600 from 10m env, got %d", opts.timeout)
	}
	if !opts.timeoutSet {
		t.Error("timeoutSet should be true when env var sets it")
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
