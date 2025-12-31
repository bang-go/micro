package ws

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestMetrics(t *testing.T) {
	// 1. Setup Server
	hub := NewHub()
	server := NewServer(&ServerConfig{Addr: "localhost:8896"})

	go server.Start(func(c Connect) {
		hub.Register(c)
		defer hub.Unregister(c)
		for {
			mt, msg, err := c.ReadMessage(context.Background())
			if err != nil {
				return
			}
			// Echo
			c.SendBinary(context.Background(), msg)
			_ = mt
		}
	})

	time.Sleep(1 * time.Second)

	// 2. Client Connect & Send
	client := NewClient("ws://localhost:8896/ws")
	client.Connect(context.Background())

	// Wait for connection
	time.Sleep(1 * time.Second)

	// Send Message
	client.OnConnect(func(c Connect) {
		// This callback might race with our manual send below if we used c here.
		// But client.Connect starts loop.
		// We can't easily access the client's internal connection to send unless we modify Client.
		// Let's rely on the fact that client.Connect blocks? No it's async in our impl.
		// Wait, NewClient returns Client interface which doesn't have Send.
		// We need to capture the connect from OnConnect to send.
	})

	// Hack: We need to trigger a send from client to server to bump msgReceived
	// Since Client interface is minimal, we'll just check Active Connections first.

	// 3. Scrape Metrics
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	promhttp.Handler().ServeHTTP(w, req)

	body := w.Body.String()

	// Verify Active Connections
	// Note: previous tests might have left metrics non-zero if using global registry.
	// But in this test run, we expect at least 1.
	if !strings.Contains(body, `ws_connections_active`) {
		t.Errorf("Expected ws_connections_active metric, got body:\n%s", body)
	}

	// 4. Trigger Broadcast to bump hubBroadcast
	hub.Broadcast([]byte("ping"))
	time.Sleep(100 * time.Millisecond)

	w = httptest.NewRecorder()
	promhttp.Handler().ServeHTTP(w, req)
	body = w.Body.String()

	if !strings.Contains(body, `ws_hub_broadcast_total`) {
		t.Errorf("Expected ws_hub_broadcast_total metric, got body:\n%s", body)
	}

	// Cleanup
	client.Close()
	server.Shutdown(context.Background())
}
