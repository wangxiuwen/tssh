package main

import (
	"net"
	"os/exec"
	"time"
)

func sleepDuration(seconds int) {
	time.Sleep(time.Duration(seconds) * time.Second)
}

func sleepMs(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func findFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 54321
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
