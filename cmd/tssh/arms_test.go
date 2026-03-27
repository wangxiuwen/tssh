package main

import (
	"strings"
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

func TestNewTabWriter(t *testing.T) {
	w := newTabWriter()
	if w == nil {
		t.Error("expected non-nil writer")
	}
}

func TestFileExists(t *testing.T) {
	// /usr/bin/open should exist on macOS
	if !fileExists("/usr/bin") {
		t.Error("/usr/bin should exist")
	}
	if fileExists("/nonexistent/path/12345") {
		t.Error("nonexistent path should not exist")
	}
}

func TestExpandShortcut(t *testing.T) {
	tests := []struct {
		input    string
		wantType string
		contains string
	}{
		{"services", "apm", "arms_app_requests_count_raw"},
		{"errors", "apm", "arms_app_requests_error_count_raw"},
		{"errors my-svc", "apm", `service="my-svc"`},
		{"latency", "apm", "arms_app_requests_seconds_raw"},
		{"slow-sql my-svc", "apm", `service="my-svc"`},
		{"cpu", "system", "arms_system_cpu_idle"},
		{"mem my-svc", "system", `service="my-svc"`},
		{"gc", "system", "arms_jvm_gc_delta"},
		{"qps my-svc", "apm", `service="my-svc"`},
		{"some_custom_metric{}", "apm", "some_custom_metric{}"},
	}
	for _, tt := range tests {
		query, dsType := expandShortcut(tt.input)
		if dsType != tt.wantType {
			t.Errorf("expandShortcut(%q) type = %q, want %q", tt.input, dsType, tt.wantType)
		}
		if !strings.Contains(query, tt.contains) {
			t.Errorf("expandShortcut(%q) = %q, want to contain %q", tt.input, query, tt.contains)
		}
	}
}

func TestPickDatasource(t *testing.T) {
	datasources := []grafana.Datasource{
		{ID: 7, Name: "arms_metrics_cn-beijing"},
		{ID: 1, Name: "metricstore-apm-metrics-detail_cn-beijing"},
		{ID: 2, Name: "metricstore-apm-metrics_cn-beijing"},
		{ID: 5, Name: "metricstore-apm-metrics-custom_cn-beijing"},
	}

	// APM type should pick detail datasource
	id := pickDatasource(datasources, "apm")
	if id != 1 {
		t.Errorf("apm: expected DS 1, got %d", id)
	}

	// System type should pick non-detail apm-metrics
	id = pickDatasource(datasources, "system")
	if id != 2 {
		t.Errorf("system: expected DS 2, got %d", id)
	}

	// Empty datasources
	id = pickDatasource(nil, "apm")
	if id != 0 {
		t.Errorf("empty: expected 0, got %d", id)
	}
}

func TestFormatMetricLabels(t *testing.T) {
	tests := []struct {
		m    map[string]string
		want string
	}{
		{nil, "{}"},
		{map[string]string{}, "{}"},
		{map[string]string{"service": "my-svc", "host": "10.0.0.1"}, "service=my-svc, host=10.0.0.1"},
		{map[string]string{"service": "svc", "__name__": "metric", "rpc": "/api"}, "service=svc, rpc=/api"},
		// Noisy labels should be filtered when priority labels exist
		{map[string]string{"service": "svc", "agentVersion": "4.6", "clusterId": "xxx"}, "service=svc"},
	}
	for _, tt := range tests {
		got := formatMetricLabels(tt.m)
		if got != tt.want {
			t.Errorf("formatMetricLabels(%v) = %q, want %q", tt.m, got, tt.want)
		}
	}
}

func TestPrintActivatedAlert_DoesNotPanic(t *testing.T) {
	alert := ActivatedAlert{
		AlertName:       "TestAlert",
		Severity:        "critical",
		Status:          "Active",
		StartsAt:        time.Now().Add(-30*time.Minute).UnixMilli(),
		IntegrationName: "ARMS_GRAFANA",
		IntegrationType: "GRAFANA",
		ExpandFields: map[string]string{
			"instance": "web-01",
			"service":  "my-service",
		},
	}
	printActivatedAlert(alert)
}

func TestPrintActivatedAlert_MinimalFields(t *testing.T) {
	alert := ActivatedAlert{
		AlertName: "Simple",
	}
	printActivatedAlert(alert)
}

func TestPrintAlert_MinimalLabels(t *testing.T) {
	alert := grafana.Alert{
		Labels:      map[string]string{"alertname": "Test"},
		Annotations: map[string]string{},
	}
	// Should not panic even with minimal data
	printAlert(alert)
}
