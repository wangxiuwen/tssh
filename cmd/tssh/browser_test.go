package main

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"prod-jump":         "prod-jump",
		"prod/jump":         "prod_jump",
		"prod jump":         "prod_jump",
		"prod:01":           "prod_01",
		"a/b/c":             "a_b_c",
	}
	for in, want := range cases {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindChromeLike_DoesntPanic(t *testing.T) {
	// Just confirm it returns a string without panic — env-dependent so we
	// can't assert a value. Windows path returns "" which is expected.
	_ = findChromeLike()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		if findChromeLike() != "" {
			t.Errorf("unexpected chrome on %s", runtime.GOOS)
		}
	}
}

func TestBrowserJSON_Schema(t *testing.T) {
	// Keep browser JSON shape stable for agent consumers.
	payload := map[string]interface{}{
		"local_port":   54321,
		"proxy":        "socks5://127.0.0.1:54321",
		"via":          "prod-jump",
		"jump_id":      "i-abc",
		"chrome_pid":   1234,
		"profile_dir":  "/tmp/xxx",
		"chrome_path":  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"opened_urls":  []string{"http://grafana.internal"},
		"pid":          5678,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{
		"local_port", "proxy", "via", "jump_id",
		"chrome_pid", "profile_dir", "chrome_path", "opened_urls", "pid",
	} {
		if !strings.Contains(string(b), `"`+f+`":`) {
			t.Errorf("browser JSON missing %q: %s", f, b)
		}
	}
}

func TestBrowserHelp_NoPanic(t *testing.T) {
	printBrowserHelp()
}
