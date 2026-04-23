package session

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wangxiuwen/tssh/internal/aliyun"
	"github.com/wangxiuwen/tssh/internal/shared"
)

// StartRemoteSocks makes sure microsocks is installed + running on 127.0.0.1
// of the remote. Returns its PID so the caller can kill it on shutdown.
// Binding loopback (not the instance's private IP) means no SOCKS5 exposure
// to the VPC even if the security group is lax — the only path in is through
// our Cloud Assistant WebSocket tunnel.
func StartRemoteSocks(client *aliyun.Client, instanceID string, port int) (string, error) {
	startCmd := fmt.Sprintf("nohup microsocks -i 127.0.0.1 -p %d >/tmp/tssh-socks.log 2>&1 & echo $!", port)

	pid, err := tryStartSocks(client, instanceID, startCmd)
	if err == nil {
		return pid, nil
	}

	// First attempt failed (likely microsocks not installed). Try to install.
	fmt.Fprintln(os.Stderr, "⚙️  安装 microsocks ...")
	installCmd := `which microsocks >/dev/null 2>&1 || {
  if command -v apt-get >/dev/null; then apt-get install -y microsocks;
  elif command -v dnf >/dev/null; then dnf install -y epel-release microsocks 2>/dev/null || dnf install -y microsocks;
  elif command -v yum >/dev/null; then yum install -y epel-release microsocks 2>/dev/null || yum install -y microsocks;
  elif command -v apk >/dev/null; then apk add --no-cache microsocks;
  else echo "no supported package manager" >&2; exit 127; fi
}`
	if _, err := client.RunCommand(instanceID, installCmd, 120); err != nil {
		return "", fmt.Errorf("install microsocks: %w", err)
	}

	pid, err = tryStartSocks(client, instanceID, startCmd)
	if err != nil {
		return "", fmt.Errorf(`microsocks 仍无法启动, 请手动安装:
    apt install microsocks      # Debian/Ubuntu
    yum install epel-release && yum install microsocks   # CentOS/RHEL
原错误: %w`, err)
	}
	return pid, nil
}

// tryStartSocks runs the start script and returns the PID string.
// An empty / non-numeric PID means the shell wrote an error (e.g.
// "microsocks: command not found") before the echo, so the caller knows to
// attempt installation.
func tryStartSocks(client *aliyun.Client, instanceID, startCmd string) (string, error) {
	res, err := client.RunCommand(instanceID, startCmd, 10)
	if err != nil {
		return "", err
	}
	pid := strings.TrimSpace(shared.DecodeOutput(res.Output))
	if pid == "" {
		return "", fmt.Errorf("empty PID")
	}
	if _, perr := strconv.Atoi(pid); perr != nil {
		return "", fmt.Errorf("invalid PID %q (microsocks 可能未安装)", pid)
	}
	return pid, nil
}
