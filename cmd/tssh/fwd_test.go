package main

import (
	"testing"
)

// Just the target-parsing logic; RDS/Redis paths hit the live API and are
// tested separately with mocks in internal/aliyun. resolveFwdTarget takes a
// *Config only to dial those APIs, so we pass nil and only exercise the
// host:port branch here.

func TestResolveFwdTarget_HostPort(t *testing.T) {
	host, port, vpc, err := resolveFwdTarget(nil, "rds-prod.internal:3306")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "rds-prod.internal" || port != 3306 {
		t.Errorf("got %s:%d", host, port)
	}
	if vpc != "" {
		t.Errorf("expected empty vpc for host:port, got %s", vpc)
	}
}

func TestResolveFwdTarget_IPPort(t *testing.T) {
	host, port, _, err := resolveFwdTarget(nil, "10.0.0.5:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "10.0.0.5" || port != 8080 {
		t.Errorf("got %s:%d", host, port)
	}
}

func TestResolveFwdTarget_InvalidPort(t *testing.T) {
	cases := []string{
		"host:99999", // port out of range
		"host:-1",    // negative
		"host:0",     // zero
		"host:abc",   // non-numeric
		"host:",      // empty port
		"noport",     // no colon
		":",          // bare colon
	}
	for _, c := range cases {
		if _, _, _, err := resolveFwdTarget(nil, c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

// IPv6 literal gets handled by LastIndex — a bare "::1:3306" parses as host
// "::1" port 3306, which is actually correct and useful. Verify.
func TestResolveFwdTarget_IPv6Loopback(t *testing.T) {
	host, port, _, err := resolveFwdTarget(nil, "::1:3306")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "::1" || port != 3306 {
		t.Errorf("got %s:%d", host, port)
	}
}
