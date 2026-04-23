package session

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wangxiuwen/tssh/internal/aliyun"
	"github.com/wangxiuwen/tssh/internal/shared"
)

// SetupSocatRelay starts `socat TCP-LISTEN:<socatPort>,fork,reuseaddr
// TCP:<remoteHost>:<remotePort>` on the jump ECS and returns the listener
// port, socat PID, and a cleanup func that kills the socat process.
// Auto-installs socat via the distro package manager on first use.
//
// Why here: this is the same pattern port-forward uses to reach a target
// that isn't the jump itself (e.g. RDS / Redis / any "10.0.0.5:80" IP).
// Keeping it next to PortForward in internal/session lets tssh-k8s kf,
// tssh-net fwd/run, etc. share the logic without importing cmd/tssh.
func SetupSocatRelay(client *aliyun.Client, jumpID, remoteHost string, remotePort int) (int, string, func(), error) {
	socatPort := shared.FindFreePortInRange(19000, 19999)
	startCmd := fmt.Sprintf("nohup socat TCP-LISTEN:%d,fork,reuseaddr TCP:'%s':%d &>/dev/null & echo $!",
		socatPort, shared.ShellQuote(remoteHost), remotePort)

	pid, err := trySocatStart(client, jumpID, startCmd)
	if err == nil {
		return socatPort, pid, mkSocatCleanup(client, jumpID, pid), nil
	}

	// Install socat then retry. Installer is idempotent + cheap.
	fmt.Fprintln(os.Stderr, "⚙️  安装 socat ...")
	_, _ = client.RunCommand(jumpID, `which socat >/dev/null 2>&1 || {
  if command -v apt-get >/dev/null; then apt-get install -y socat;
  elif command -v dnf >/dev/null; then dnf install -y socat;
  elif command -v yum >/dev/null; then yum install -y socat;
  elif command -v apk >/dev/null; then apk add --no-cache socat;
  else exit 127; fi
}`, 120)
	pid, err = trySocatStart(client, jumpID, startCmd)
	if err != nil {
		return 0, "", nil, fmt.Errorf("socat 仍无法启动: %w", err)
	}
	return socatPort, pid, mkSocatCleanup(client, jumpID, pid), nil
}

// trySocatStart runs the start script once and validates the PID echo.
// A non-numeric PID means socat wasn't found (the shell error went to stdout
// ahead of "echo $!"), so the caller should install and retry.
func trySocatStart(client *aliyun.Client, jumpID, startCmd string) (string, error) {
	res, err := client.RunCommand(jumpID, startCmd, 10)
	if err != nil {
		return "", err
	}
	pid := strings.TrimSpace(shared.DecodeOutput(res.Output))
	if pid == "" {
		return "", fmt.Errorf("empty PID")
	}
	if _, perr := strconv.Atoi(pid); perr != nil {
		return "", fmt.Errorf("non-numeric PID %q (socat 可能未安装)", pid)
	}
	return pid, nil
}

func mkSocatCleanup(client *aliyun.Client, jumpID, pid string) func() {
	return func() {
		if pid == "" {
			return
		}
		_, _ = client.RunCommand(jumpID, fmt.Sprintf("kill %s 2>/dev/null", shared.ShellQuote(pid)), 5)
	}
}
