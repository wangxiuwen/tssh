package main

import (
	"testing"
	"time"

	"github.com/wangxiuwen/tssh/internal/grafana"
)

func TestSeverityOrder(t *testing.T) {
	tests := []struct {
		severity string
		want     int
	}{
		{"critical", 0},
		{"Critical", 0},
		{"warning", 1},
		{"info", 2},
		{"unknown", 3},
		{"", 3},
	}
	for _, tt := range tests {
		got := severityOrder(tt.severity)
		if got != tt.want {
			t.Errorf("severityOrder(%q) = %d, want %d", tt.severity, got, tt.want)
		}
	}
}

func TestSeverityIcon(t *testing.T) {
	tests := []struct {
		severity string
		want     string
	}{
		{"critical", "🔴"},
		{"warning", "🟡"},
		{"info", "🔵"},
		{"other", "⚪"},
	}
	for _, tt := range tests {
		got := severityIcon(tt.severity)
		if got != tt.want {
			t.Errorf("severityIcon(%q) = %q, want %q", tt.severity, got, tt.want)
		}
	}
}

func TestHasFlag(t *testing.T) {
	args := []string{"-j", "--verbose", "hello"}
	if !hasFlag(args, "-j", "--json") {
		t.Error("expected -j to match")
	}
	if hasFlag(args, "--json") {
		t.Error("--json should not match")
	}
	if !hasFlag(args, "--verbose") {
		t.Error("--verbose should match")
	}
}

func TestGetNonFlagArg(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"-j", "hello"}, "hello"},
		{[]string{"-j", "--verbose"}, ""},
		{[]string{"query"}, "query"},
		{nil, ""},
	}
	for _, tt := range tests {
		got := getNonFlagArg(tt.args)
		if got != tt.want {
			t.Errorf("getNonFlagArg(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestPrintAlert_DoesNotPanic(t *testing.T) {
	alert := grafana.Alert{
		Labels: map[string]string{
			"alertname": "TestAlert",
			"severity":  "critical",
			"instance":  "web-01",
		},
		Annotations: map[string]string{
			"summary":     "Something is wrong",
			"description": "Detailed explanation",
		},
		StartsAt: time.Now().Add(-30 * time.Minute),
		Status:   grafana.AlertStatus{State: "active"},
	}
	// Should not panic
	printAlert(alert)
}

func TestPrintAlert_MinimalLabels(t *testing.T) {
	alert := grafana.Alert{
		Labels:      map[string]string{"alertname": "Test"},
		Annotations: map[string]string{},
	}
	// Should not panic even with minimal data
	printAlert(alert)
}
