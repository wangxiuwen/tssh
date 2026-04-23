package net

import (
	"runtime"
	"strings"
	"testing"
)

func TestParseCIDRList(t *testing.T) {
	cases := map[string][]string{
		"":                                  nil,
		"10.0.0.0/16":                       {"10.0.0.0/16"},
		"10.0.0.0/16,172.16.0.0/12":         {"10.0.0.0/16", "172.16.0.0/12"},
		"  10.0.0.0/16  ,  172.16.0.0/12  ": {"10.0.0.0/16", "172.16.0.0/12"},
		",,10.0.0.0/16,":                    {"10.0.0.0/16"},
	}
	for in, want := range cases {
		got := parseCIDRList(in)
		if len(got) != len(want) {
			t.Errorf("parseCIDRList(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("parseCIDRList(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}

func TestDefaultTunName(t *testing.T) {
	name := defaultTunName()
	if runtime.GOOS == "darwin" && name != "utun" {
		t.Errorf("darwin default: got %q, want utun", name)
	}
	if runtime.GOOS == "linux" && name != "tssh0" {
		t.Errorf("linux default: got %q, want tssh0", name)
	}
}

func TestRouteAddCmd_Linux(t *testing.T) {
	// We can't change runtime.GOOS, so test whichever platform we're on and
	// verify the add/del inversion is consistent (del should negate add).
	add, del := routeAddCmd("tssh0", "10.0.0.0/16")
	if len(add) == 0 || len(del) == 0 {
		t.Fatal("empty argv")
	}
	// First token must be an executable name (ip or route), not a flag.
	if strings.HasPrefix(add[0], "-") {
		t.Errorf("add argv looks malformed: %v", add)
	}
	// del must share the same executable + CIDR as add.
	if add[0] != del[0] {
		t.Errorf("add/del use different tools: %s vs %s", add[0], del[0])
	}
	joined := strings.Join(del, " ")
	if !strings.Contains(joined, "10.0.0.0/16") {
		t.Errorf("del argv missing CIDR: %v", del)
	}
	// Verify "add" verb is present in add argv, some form of remove in del.
	addJoined := strings.Join(add, " ")
	if !strings.Contains(addJoined, "add") {
		t.Errorf("add argv missing 'add' verb: %v", add)
	}
	if !(strings.Contains(joined, "del") || strings.Contains(joined, "delete")) {
		t.Errorf("del argv missing delete verb: %v", del)
	}
}

func TestVPNHelp_NoPanic(t *testing.T) {
	printVPNHelp()
}
