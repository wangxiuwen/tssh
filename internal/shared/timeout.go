package shared

import (
	"fmt"
	"strconv"
	"time"
)

// ParseTimeoutSec accepts a bare integer ("300") OR a Go duration ("5m",
// "2h30m") and returns seconds. Returns an error (not zero) for invalid
// input; callers should never see 0 silently when a user's --timeout 5m
// typo would otherwise collapse into a 10s client poll. See the commit
// history that added it — several bugs boiled down to "Atoi ignored".
func ParseTimeoutSec(s string) (int, error) {
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("timeout 必须大于 0: %s", s)
		}
		return n, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("无法解析 timeout: %q (期望整数秒或 Go duration, 如 300 / 5m / 2h)", s)
	}
	sec := int(d.Seconds())
	if sec <= 0 {
		return 0, fmt.Errorf("timeout 必须大于 0: %s", s)
	}
	return sec, nil
}
