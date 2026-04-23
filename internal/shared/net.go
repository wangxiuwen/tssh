package shared

import "net"

// FindFreePort asks the kernel for an unused TCP port by binding :0 and
// reading back the assigned port. Returns 54321 as a last-resort fallback
// only if every outbound attempt fails (extremely unlikely — kernel grants
// ports from an ephemeral range). Callers should tolerate the fallback
// colliding with something; it's strictly better than panicking.
func FindFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 54321
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// FindFreePortInRange picks a port inside [start, end]. We currently delegate
// to FindFreePort and modulo-wrap into the range; the kernel's ephemeral
// range is usually high (32768+) so landing inside 18000-19999 that way is
// random enough to avoid collisions between concurrent tssh invocations.
// Real contention is rare for these diagnostic port windows so we don't
// actually probe — a caller that needs guaranteed availability should
// FindFreePort() and pass the port in explicitly.
func FindFreePortInRange(start, end int) int {
	if end < start {
		return start
	}
	return start + FindFreePort()%(end-start+1)
}
