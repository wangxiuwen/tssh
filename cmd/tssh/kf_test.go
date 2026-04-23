package main

import (
	"testing"
)

func TestParseKFTarget_SvcPort(t *testing.T) {
	got, err := parseKFTarget("grafana:80")
	if err != nil {
		t.Fatal(err)
	}
	if got.svc != "grafana" || got.svcPort != 80 || got.localPort != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestParseKFTarget_FixedLocalPort(t *testing.T) {
	got, err := parseKFTarget("grafana:80=3000")
	if err != nil {
		t.Fatal(err)
	}
	if got.localPort != 3000 {
		t.Errorf("localPort: %+v", got)
	}
}

func TestParseKFTarget_SvcPrefix(t *testing.T) {
	// Users mix `svc/grafana:80` and `grafana:80`; both should work.
	got, err := parseKFTarget("svc/grafana:80")
	if err != nil {
		t.Fatal(err)
	}
	if got.svc != "grafana" {
		t.Errorf("svc prefix not stripped: %+v", got)
	}
}

func TestParseKFTarget_Errors(t *testing.T) {
	cases := []string{
		"",
		"noport",
		"foo:",
		":80",
		"foo:abc",
		"foo:99999",         // out of range
		"foo:80=notanumber",
		"foo:80=0",
	}
	for _, c := range cases {
		if _, err := parseKFTarget(c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestParseKFTargets_Multiple(t *testing.T) {
	ts, err := parseKFTargets([]string{"grafana:80", "prom:9090=9091"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 {
		t.Fatalf("expected 2, got %d", len(ts))
	}
	if ts[1].svc != "prom" || ts[1].svcPort != 9090 || ts[1].localPort != 9091 {
		t.Errorf("target[1]: %+v", ts[1])
	}
}

func TestKFHelp_NoPanic(t *testing.T) {
	printKFHelp()
}
