package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// startPortForward launches ali-instance-cli portforward with credentials via environment variables.
// Security: credentials are passed via env vars, not visible in ps aux.
func startPortForward(cfg *Config, instanceID string, localPort, remotePort int) *exec.Cmd {
	cmd := execCommand("ali-instance-cli", "portforward",
		"--instance", instanceID,
		"--local-port", strconv.Itoa(localPort),
		"--remote-port", strconv.Itoa(remotePort),
		"--region", cfg.Region,
	)
	cmd.Env = append(os.Environ(),
		"ALIBABA_CLOUD_ACCESS_KEY_ID="+cfg.AccessKeyID,
		"ALIBABA_CLOUD_ACCESS_KEY_SECRET="+cfg.AccessKeySecret,
	)
	return cmd
}

// startPortForwardBg starts portforward in background and waits for connection
func startPortForwardBg(cfg *Config, instanceID string, localPort, remotePort int) (*exec.Cmd, error) {
	cmd := startPortForward(cfg, instanceID, localPort, remotePort)
	cmd.Stderr = nil
	cmd.Stdout = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("portforward failed: %w", err)
	}
	sleepMs(3000)
	return cmd, nil
}
