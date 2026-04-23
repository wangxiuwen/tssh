package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
)

// Exercise wsSafeWriter under real concurrent pressure: 4 goroutines writing
// from a shared connection. Prior to the fix, gorilla/websocket would panic
// with "concurrent write to websocket connection". The -race detector would
// also flag unsynchronized writes to the underlying net.Conn buffer.
func TestWsSafeWriter_Concurrent(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var received int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
			atomic.AddInt64(&received, 1)
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	send := wsSafeWriter(conn)

	const goroutines = 4
	const iters = 100
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				send([]byte("payload"))
			}
		}()
	}
	wg.Wait()
	// No panic, no race-detector report == pass.
	// We don't assert exact count because the read side can be lossy on close.
}
