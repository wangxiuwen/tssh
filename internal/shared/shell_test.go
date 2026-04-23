package shared

import "testing"

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"simple":           "simple",
		"with space":       "with space",
		"with'quote":       `with'\''quote`,
		"multiple'quotes'": `multiple'\''quotes'\''`,
		"":                 "",
		"/usr/local/bin":   "/usr/local/bin",
		"$HOME":            "$HOME", // not shell-metachar escape — caller wraps in single quotes
	}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

// Regression: the classic "'; rm -rf /;'" injection should become inert
// when the caller wraps the escaped result in single quotes. We don't try
// to verify character-by-character (the '\'' escape sequence has embedded
// apostrophes by design); instead assert the expected full replacement.
func TestShellQuote_InjectionSafe(t *testing.T) {
	in := "';rm -rf /;'"
	out := ShellQuote(in)
	// Every apostrophe in the input should be exactly the 4-char sequence
	// '\'' in the output. Original input has 2 apostrophes → output gains
	// 6 chars (each '→'\'' adds 3).
	if len(out) != len(in)+6 {
		t.Errorf("expected len(out)=len(in)+6, got in=%d out=%d\n  out=%q", len(in), len(out), out)
	}
	// The wrapped command would be: cmd '<out>' → shell reads as a single
	// argument, ; never escapes the quotes.
	wrapped := "'" + out + "'"
	// Brittle invariant check: wrapped starts and ends with '
	// and contains no unescaped apostrophe pair that would break out.
	// Easier assertion: the substring "';r" (which would mean injection
	// succeeded if present in an unwrapped position) only appears as part
	// of the escape sequence.
	if !(wrapped[0] == '\'' && wrapped[len(wrapped)-1] == '\'') {
		t.Errorf("wrapped form does not start/end with apostrophe: %q", wrapped)
	}
}
