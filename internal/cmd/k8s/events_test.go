package k8s

import (
	"strings"
	"testing"
)

func TestRegexEscape(t *testing.T) {
	cases := map[string]string{
		"grafana":        "grafana",
		"my-svc":         "my-svc", // hyphen is regex-literal, unchanged
		"my.svc":         `my\.svc`,
		"foo*bar":        `foo\*bar`,
		"foo|bar":        `foo\|bar`,
		`foo\bar`:        `foo\\bar`,
		"(a)":            `\(a\)`,
	}
	for in, want := range cases {
		if got := regexEscape(in); got != want {
			t.Errorf("regexEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegexEscape_NoInjection(t *testing.T) {
	// Pathological inputs should not produce awk-special chars in the output
	// that change the meaning of the filter. Catch any special chars we
	// forgot by rescanning the output — nothing special should appear
	// unescaped.
	out := regexEscape(`$a.b+c|d(e)`)
	// each special must be preceded by \ (or be itself a backslash).
	for i := 0; i < len(out); i++ {
		c := out[i]
		if strings.ContainsRune(`$.+|()`, rune(c)) {
			if i == 0 || out[i-1] != '\\' {
				t.Errorf("special char %q at pos %d not escaped: %q", c, i, out)
			}
		}
	}
}

func TestEventsHelp_NoPanic(t *testing.T) {
	printEventsHelp()
}
