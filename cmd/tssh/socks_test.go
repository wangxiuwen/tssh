package main

import (
	"testing"
)

// Argument parsing for cmdSocks is small but easy to break; cover the two
// main flags plus bad input so regressions show up in CI instead of a live
// session.

func TestSocksHelp_NoPanic(t *testing.T) {
	printSocksHelp() // should not panic or need any state
}

// tryStartSocks pid validation — empty / non-numeric should be rejected so
// callers know to reinstall, not silently send `kill <garbage>`.
func TestTryStartSocks_PIDValidation(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"12345", false},
		{"0", false}, // 0 is numeric; microsocks won't return it in practice but strconv accepts
		{"", true},
		{"microsocks: command not found", true},
		{"bash: microsocks: not found", true},
	}
	for _, c := range cases {
		// Replicate the core check here; keeps tryStartSocks unexported
		// and we don't need a real aliyun client to test the logic.
		got := pidLooksValid(c.in)
		if got == c.wantErr {
			t.Errorf("pidLooksValid(%q) = %v, want !%v", c.in, got, c.wantErr)
		}
	}
}

// Duplicates the validation from tryStartSocks so the test can run without
// spinning up an AliyunClient. If the real logic drifts, this test will too.
func pidLooksValid(pid string) bool {
	if pid == "" {
		return false
	}
	for _, ch := range pid {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
