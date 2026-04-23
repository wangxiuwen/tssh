package main

import (
	"testing"
)

func TestParseRunSpec_Basic(t *testing.T) {
	targets, err := parseRunSpec("mysql=rm-xxx,redis=r-yyy,kafka=10.0.0.3:9092")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(targets))
	}
	if targets[0].name != "mysql" || targets[0].raw != "rm-xxx" {
		t.Errorf("target 0: %+v", targets[0])
	}
	if targets[1].name != "redis" || targets[1].raw != "r-yyy" {
		t.Errorf("target 1: %+v", targets[1])
	}
	if targets[2].name != "kafka" || targets[2].raw != "10.0.0.3:9092" {
		t.Errorf("target 2: %+v", targets[2])
	}
}

func TestParseRunSpec_WhitespaceTolerant(t *testing.T) {
	targets, err := parseRunSpec("  mysql = rm-xxx , redis= r-yyy ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 || targets[0].raw != "rm-xxx" || targets[1].raw != "r-yyy" {
		t.Errorf("got %+v", targets)
	}
}

func TestParseRunSpec_Errors(t *testing.T) {
	cases := map[string]string{
		"":           "empty",
		"=rm-xxx":    "missing key",
		"mysql=":     "missing value",
		"foo bar":    "no equals",
		"a=1,a=2":    "duplicate key",
		"1foo=x":     "key starting with digit",
		"foo.bar=x":  "invalid char in key",
	}
	for spec, why := range cases {
		if _, err := parseRunSpec(spec); err == nil {
			t.Errorf("spec %q (%s) should have errored", spec, why)
		}
	}
}

func TestEnvPrefix(t *testing.T) {
	cases := map[string]string{
		"mysql":         "MYSQL",
		"redis-cache":   "REDIS_CACHE",
		"kafka_broker":  "KAFKA_BROKER",
		"api_v2":        "API_V2",
	}
	for in, want := range cases {
		if got := envPrefix(in); got != want {
			t.Errorf("envPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsValidEnvName(t *testing.T) {
	ok := []string{"mysql", "redis_cache", "redis-cache", "api2", "a", "A"}
	bad := []string{"", "1foo", "foo.bar", "foo bar", "foo/bar", "$foo"}
	for _, s := range ok {
		if !isValidEnvName(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	for _, s := range bad {
		if isValidEnvName(s) {
			t.Errorf("expected %q invalid", s)
		}
	}
}
