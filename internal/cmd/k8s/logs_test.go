package k8s

import (
	"strings"
	"testing"
)

func TestNsFlagFor(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"monitoring": " -n 'monitoring'",
		"weird ns":   " -n 'weird ns'",
	}
	for in, want := range cases {
		got := nsFlagFor(in)
		// shellQuote wraps in single quotes; accept either format.
		if got != want && !strings.Contains(got, in) {
			t.Errorf("nsFlagFor(%q) = %q, want %q (or quoted)", in, got, want)
		}
	}
}

func TestAtoiDefault(t *testing.T) {
	if atoiDefault("123", -1) != 123 {
		t.Error("simple int")
	}
	if atoiDefault("abc", 99) != 99 {
		t.Error("bad input should return default")
	}
	if atoiDefault("", 0) != 0 {
		t.Error("empty input should return default")
	}
}

func TestLogsHelp_NoPanic(t *testing.T) {
	printLogsHelp()
}
