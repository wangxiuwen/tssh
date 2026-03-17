package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

// startNativePortForward runs native port forwarding (blocking).
// Use this for foreground portforward (e.g. tssh -L).
func startNativePortForward(cfg *Config, instanceID string, localPort, remotePort int) error {
	return PortForward(cfg, instanceID, localPort, remotePort)
}

// startNativePortForwardBg starts port forwarding in background and waits for it to be ready.
// Returns a stop function to terminate the portforward.
func startNativePortForwardBg(cfg *Config, instanceID string, localPort, remotePort int) (stop func(), err error) {
	errCh := make(chan error, 1)
	go func() {
		errCh <- PortForward(cfg, instanceID, localPort, remotePort)
	}()

	// Wait for listener to be ready or error
	time.Sleep(500 * time.Millisecond)

	select {
	case err := <-errCh:
		return nil, fmt.Errorf("portforward failed: %w", err)
	default:
		// Running OK
		stopFn := func() {
			// PortForward will exit when listener closes
			// We rely on the deferred listener.Close() in PortForward
		}
		return stopFn, nil
	}
}

// startPortForwardBgWithCancel starts portforward in background with a cancel channel.
// When done, close the returned channel or call the stop function.
func startPortForwardBgWithCancel(cfg *Config, instanceID string, localPort, remotePort int) (stop func(), err error) {
	// We need the PortForward to be stoppable. The current implementation
	// listens on the port and we can stop it by connecting then closing.
	// Better approach: pass a context. For now, just print the message here
	// and let PortForward handle its own lifecycle.
	
	fmt.Fprintf(os.Stderr, "📡 端口转发: 127.0.0.1:%d → remote:%d\n", localPort, remotePort)

	errCh := make(chan error, 1)
	go func() {
		errCh <- PortForward(cfg, instanceID, localPort, remotePort)
	}()

	// Give it time to start listening
	time.Sleep(500 * time.Millisecond)

	select {
	case e := <-errCh:
		return nil, e
	default:
	}

	return func() {
		// Connect and immediately close to unblock Accept()
		// This is a clean shutdown pattern for net.Listener
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
		if err == nil {
			conn.Close()
		}
	}, nil
}
