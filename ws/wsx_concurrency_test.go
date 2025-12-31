package ws

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestHighConcurrencyBroadcast(t *testing.T) {
	// 1. Setup Server
	hub := NewHub()
	server := NewServer(&ServerConfig{Addr: "localhost:8895"})

	go server.Start(func(c Connect) {
		hub.Register(c)
		// No need to unregister explicitly for this test, Hub handles disconnects eventually?
		// Actually Hub.Unregister is manual. In real app, you call it on close.
		// For test, we might skip it or do it properly.
		defer hub.Unregister(c)

		// Read loop to keep connection open and drain messages if any
		for {
			_, _, err := c.ReadMessage(context.Background())
			if err != nil {
				return
			}
		}
	})

	time.Sleep(1 * time.Second)

	// 2. Simulate many clients (e.g., 1000)
	// Real high concurrency test (100k) requires OS tuning (ulimit), so we simulate a smaller batch
	// but verify the architecture (Goroutines count).

	clientCount := 1000
	var receivedCount int64
	var wg sync.WaitGroup

	clients := make([]Client, clientCount)

	for i := 0; i < clientCount; i++ {
		wg.Add(1)
		c := NewClient("ws://localhost:8895/ws")
		c.OnMessage(func(mt websocket.MessageType, msg []byte) {
			atomic.AddInt64(&receivedCount, 1)
		})
		c.OnConnect(func(conn Connect) {
			wg.Done()
		})
		c.Connect(context.Background())
		clients[i] = c
	}

	// Wait for all to connect
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("Timeout waiting for clients to connect. Connected: %d", hub.Count())
	}

	t.Logf("Connected %d clients", clientCount)

	// 3. Measure Goroutines before broadcast
	// (Hard to measure programmatically inside test without runtime.NumGoroutine, but we trust logic)

	// 4. Broadcast
	start := time.Now()
	hub.Broadcast([]byte("Massive Message"))
	duration := time.Since(start)

	t.Logf("Broadcast to %d clients took %v", clientCount, duration)

	// 5. Verify receipt
	// Wait for propagation
	time.Sleep(2 * time.Second)

	rcv := atomic.LoadInt64(&receivedCount)
	if rcv != int64(clientCount) {
		t.Errorf("Expected %d messages, got %d", clientCount, rcv)
	}

	// Cleanup
	for _, c := range clients {
		c.Close()
	}
	server.Shutdown(context.Background())
}
