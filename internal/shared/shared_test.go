package shared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindFreePort(t *testing.T) {
	p := FindFreePort()
	if p <= 0 || p > 65535 {
		t.Errorf("FindFreePort() = %d, want 1..65535", p)
	}
	// Calling twice should return different (or at least valid) ports —
	// kernel rarely re-hands-out the same ephemeral port in back-to-back calls.
	p2 := FindFreePort()
	if p2 <= 0 || p2 > 65535 {
		t.Errorf("second call invalid: %d", p2)
	}
}

func TestFindFreePortInRange(t *testing.T) {
	// Sample many times; every result must land inside [start, end].
	for i := 0; i < 10; i++ {
		p := FindFreePortInRange(18000, 18999)
		if p < 18000 || p > 18999 {
			t.Errorf("out of range: %d", p)
		}
	}
	// Degenerate range returns start.
	if FindFreePortInRange(5000, 4000) != 5000 {
		t.Error("end<start should yield start")
	}
	if FindFreePortInRange(5555, 5555) != 5555 {
		t.Error("single-port range should yield that port")
	}
}

func TestDecodeOutput_Base64(t *testing.T) {
	// base64("hello") = "aGVsbG8="
	if got := DecodeOutput("aGVsbG8="); got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
	// Whitespace is trimmed.
	if got := DecodeOutput("  aGVsbG8=  \n"); got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestDecodeOutput_Fallback(t *testing.T) {
	// Not base64 → return as-is.
	if got := DecodeOutput("plain text"); got != "plain text" {
		t.Errorf("should fall back, got %q", got)
	}
}

func TestTruncateStr(t *testing.T) {
	cases := map[string]struct {
		in   string
		max  int
		want string
	}{
		"short":        {"abc", 10, "abc"},
		"exact":        {"abcdef", 6, "abcdef"},
		"overflow":     {"abcdefghij", 5, "abcde..."},
		"with newline": {"a\nb", 10, "a\\nb"},
	}
	for name, c := range cases {
		if got := TruncateStr(c.in, c.max); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	if FileExists(f) {
		t.Error("should not exist")
	}
	if err := os.WriteFile(f, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if !FileExists(f) {
		t.Error("should exist after write")
	}
}

func TestIsTerminal_NoPanic(t *testing.T) {
	// Under `go test` stdin is not a TTY, so it should return false —
	// but mainly we want "doesn't panic on stdin being a file."
	_ = IsTerminal()
}

func TestParseTimeoutSec(t *testing.T) {
	cases := map[string]int{
		"60":    60,
		"300":   300,
		"5m":    300,
		"2h":    7200,
		"1h30m": 5400,
		"300s":  300,
	}
	for in, want := range cases {
		got, err := ParseTimeoutSec(in)
		if err != nil {
			t.Errorf("ParseTimeoutSec(%q): unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseTimeoutSec(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseTimeoutSec_Errors(t *testing.T) {
	for _, bad := range []string{"", "abc", "-5", "0", "5x", "500ms"} {
		if _, err := ParseTimeoutSec(bad); err == nil {
			t.Errorf("ParseTimeoutSec(%q) should have errored", bad)
		}
	}
}

// Regression: a helper message should mention something actionable for the
// user, not just the raw strconv error.
func TestParseTimeoutSec_HelpfulError(t *testing.T) {
	_, err := ParseTimeoutSec("five minutes")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "300") || !strings.Contains(err.Error(), "5m") {
		t.Errorf("error missing examples: %v", err)
	}
}
