package main

import (
	"strings"
	"testing"
)

// Parse a realistic-shaped script output and check every field lands in the
// right place. Protects against regressions in the line format chosen on the
// remote side (those can't be changed without updating this test, which is
// the point).

func TestParseKSOutput_Full(t *testing.T) {
	raw := `svc=grafana
ns=monitoring
selector=app=grafana
type=ClusterIP
cluster_ip=10.0.100.42
ports=80/TCP
endpoints_alive=3
---pods---
pod=grafana-abc-1|phase=Running|ready=true|ip=10.1.1.10|node=node-a|estab=42|tw=120|cpu=15m|mem=80Mi
pod=grafana-abc-2|phase=Running|ready=true|ip=10.1.1.11|node=node-b|estab=31|tw=95|cpu=12m|mem=76Mi
pod=grafana-abc-3|phase=Running|ready=false|ip=10.1.1.12|node=node-c|estab=0|tw=8
---summary---
pod_total=3
pod_ready=2
`
	d := parseKSOutput(raw)
	if d.Service != "grafana" || d.Namespace != "monitoring" || d.Type != "ClusterIP" {
		t.Errorf("top-level fields wrong: %+v", d)
	}
	if d.EndpointsAlive != 3 || d.PodTotal != 3 || d.PodReady != 2 {
		t.Errorf("counts: %+v", d)
	}
	if len(d.Pods) != 3 {
		t.Fatalf("expected 3 pods, got %d", len(d.Pods))
	}
	if d.Pods[0].Estab != 42 || d.Pods[0].TW != 120 {
		t.Errorf("pod[0] stats: %+v", d.Pods[0])
	}
	if d.Pods[0].CPU != "15m" || d.Pods[0].Mem != "80Mi" {
		t.Errorf("pod[0] resources: %+v", d.Pods[0])
	}
	if d.Pods[2].Ready || d.Pods[2].Estab != 0 {
		t.Errorf("pod[2]: %+v", d.Pods[2])
	}
	// Pod 3 has no CPU/MEM (metrics-server missing) — should be empty strings
	if d.Pods[2].CPU != "" || d.Pods[2].Mem != "" {
		t.Errorf("pod[2] should have empty CPU/Mem: %+v", d.Pods[2])
	}
}

func TestParseKSOutput_SvcNotFound(t *testing.T) {
	raw := "svc=mystery\nns=default\nerr=svc_not_found\n"
	d := parseKSOutput(raw)
	if d.Err != "svc_not_found" {
		t.Errorf("expected err flag, got %+v", d)
	}
}

func TestParseKSOutput_NoPods(t *testing.T) {
	raw := "svc=foo\nns=default\nselector=app=foo\npod_total=0\npod_ready=0\n"
	d := parseKSOutput(raw)
	if len(d.Pods) != 0 || d.PodTotal != 0 {
		t.Errorf("expected zero pods, got %+v", d)
	}
}

func TestDefaultStr(t *testing.T) {
	if defaultStr("", "x") != "x" {
		t.Errorf("empty should fall back")
	}
	if defaultStr("y", "x") != "y" {
		t.Errorf("non-empty should pass through")
	}
}

func TestKSHelp_NoPanic(t *testing.T) {
	printKSHelp()
}

// Regression: line endings / trimming. Remote script might produce \r\n if
// Cloud Assistant encoding surprises us; the parser should handle both.
func TestParseKSOutput_CRLF(t *testing.T) {
	raw := "svc=foo\r\nns=default\r\npod_total=1\r\npod_ready=1\r\n"
	d := parseKSOutput(raw)
	if d.Service != "foo" {
		t.Errorf("CRLF not handled, got service=%q", d.Service)
	}
	if d.PodTotal != 1 {
		t.Errorf("CRLF broke numeric parse: %+v", d)
	}
	// Also confirm no trailing \r in Service value (TrimSpace should catch \r).
	if strings.Contains(d.Service, "\r") {
		t.Errorf("trailing \\r leaked: %q", d.Service)
	}
}
