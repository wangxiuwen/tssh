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
		StartsAt:        time.Now().Add(-30 * time.Minute).UnixMilli(),
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

func TestFormatMs(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0ms"},
		{500, "500ms"},
		{999, "999ms"},
		{1000, "1.00s"},
		{1250, "1.25s"},
		{12345, "12.35s"},
	}
	for _, tt := range tests {
		if got := formatMs(tt.in); got != tt.want {
			t.Errorf("formatMs(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsInterestingTag(t *testing.T) {
	keep := []string{"globalId", "http.url", "http.status", "db.statement", "userId", "requestId", "errorCode", "traceId"}
	drop := []string{"agentVersion", "clusterName", "envName", "pid"}
	for _, k := range keep {
		if !isInterestingTag(k) {
			t.Errorf("expected %q to be interesting", k)
		}
	}
	for _, k := range drop {
		if isInterestingTag(k) {
			t.Errorf("expected %q NOT to be interesting", k)
		}
	}
}

func TestSpanIcon(t *testing.T) {
	cases := []struct {
		span TraceSpan
		want string
	}{
		{TraceSpan{RpcType: 0, ResultCode: "00"}, "🌐"},
		{TraceSpan{RpcType: 2, ResultCode: "00"}, "🗄️ "},
		{TraceSpan{RpcType: 3, ResultCode: "SUCCESS"}, "📨"},
		{TraceSpan{RpcType: 0, ResultCode: "500"}, "❌"},
	}
	for _, c := range cases {
		if got := spanIcon(c.span); got != c.want {
			t.Errorf("spanIcon(%+v) = %q, want %q", c.span, got, c.want)
		}
	}
}

func TestPrintTraceSpans_DoesNotPanic(t *testing.T) {
	spans := []TraceSpan{
		{TraceID: "abc", RpcID: "0", OperationName: "entry", ServiceName: "svc", ServiceIp: "1.1.1.1", Duration: 100, ResultCode: "00",
			TagEntryList: []TraceTag{{Key: "globalId", Value: "xyz"}, {Key: "agentVersion", Value: "x"}}},
		{TraceID: "abc", RpcID: "0.1", OperationName: "db", ServiceName: "svc", ServiceIp: "1.1.1.1", Duration: 50, ResultCode: "500"},
	}
	printTraceSpans("abc", spans)
}

func TestPrintTraceSummary_DoesNotPanic(t *testing.T) {
	traces := []TraceInfo{
		{TraceID: "abc", ServiceName: "a", ServiceIp: "1.1.1.1", OperationName: "/x", Duration: 120, Timestamp: 1700000000000},
		{TraceID: "def", ServiceName: "b", ServiceIp: "1.1.1.2", OperationName: "/y/z/very/long/path/for/truncation/test", Duration: 1500, Timestamp: 1700000001000},
	}
	printTraceSummary(traces)
}

func TestFormatTags(t *testing.T) {
	// Single-key case is order-stable, multi-key we just check non-empty.
	if got := formatTags(map[string]string{"globalId": "abc"}); got != "globalId=abc" {
		t.Errorf("single: got %q", got)
	}
	if got := formatTags(map[string]string{"a": "1", "b": "2"}); got == "" {
		t.Error("multi: should not be empty")
	}
}
