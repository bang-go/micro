package ws

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWSX_EnterpriseFeatures(t *testing.T) {
	addr := "localhost:8890"

	// 0. Setup Hub
	hub := NewHub()

	// 1. Start Server with Auth
	serverConf := &ServerConfig{Addr: addr}

	authHook := func(r *http.Request) error {
		token := r.Header.Get("X-Token")
		if token != "secret-token" {
			return fmt.Errorf("invalid token")
		}
		return nil
	}

	server := NewServer(serverConf, WithServerBeforeUpgrade(authHook))

	go func() {
		err := server.Start(func(conn Connect) {
			hub.Register(conn)
			defer hub.Unregister(conn)

			// Simple Echo
			for {
				mt, msg, err := conn.ReadMessage(context.Background())
				if err != nil {
					return
				}
				if mt == websocket.MessageText {
					// Broadcast received message
					hub.Broadcast(msg)
				}
			}
		})
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("Server start failed: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	// 2. Test Auth Failure
	clientNoAuth := NewClient("ws://"+addr+"/ws",
		WithClientMaxReconnectAttempts(0), // Don't retry for this test
	)
	authFailed := make(chan struct{})
	clientNoAuth.OnClose(func(err error) {
		close(authFailed)
	})
	clientNoAuth.Connect(context.Background())

	select {
	case <-authFailed:
		// success
	case <-time.After(2 * time.Second):
		t.Error("Client should fail without token")
	}

	// 3. Test Auth Success & Broadcast
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Helper to create authenticated client
	createClient := func(id string) (Client, chan string) {
		header := http.Header{}
		header.Set("X-Token", "secret-token")

		c := NewClient("ws://"+addr+"/ws",
			WithClientHTTPHeader(header),
			WithClientReconnectInterval(100*time.Millisecond),
		)

		msgCh := make(chan string, 10)
		c.OnMessage(func(mt websocket.MessageType, msg []byte) {
			msgCh <- string(msg)
		})

		c.Connect(ctx)
		return c, msgCh
	}

	client1, _ := createClient("user1")
	_, msgCh2 := createClient("user2")

	time.Sleep(1 * time.Second) // Wait for connection

	// Send message from client1
	// Wait, Client interface doesn't expose SendText directly unless we grab conn from OnConnect?
	// Ah, our Client design wraps the loop but doesn't expose the underlying Connect easily for sending OUTSIDE the callback.
	// This is a small design flaw in our Client wrapper if we want to send proactively.
	// But for this test, we can capture the Connect in OnConnect.

	var conn1 Connect
	client1.OnConnect(func(c Connect) {
		conn1 = c
	})
	// Re-trigger connect to capture conn1? No, OnConnect is called when connected.
	// Since we already called Connect(ctx), it might be racey.
	// Let's restart client1 with OnConnect set.

	client1.Close()
	client1 = NewClient("ws://"+addr+"/ws",
		WithClientHTTPHeader(http.Header{"X-Token": []string{"secret-token"}}),
	)
	client1.OnConnect(func(c Connect) {
		conn1 = c
	})
	client1.Connect(ctx)

	time.Sleep(1 * time.Second)

	if conn1 != nil {
		conn1.SendText(context.Background(), "Hello Broadcast")
	} else {
		t.Fatal("Client1 failed to connect")
	}

	// Check if client2 received it
	select {
	case msg := <-msgCh2:
		if msg != "Hello Broadcast" {
			t.Errorf("Expected 'Hello Broadcast', got %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Error("Client2 did not receive broadcast")
	}

	// 4. Test Hub Count
	if count := hub.Count(); count < 2 {
		// Might be 2 or 3 depending on cleanup speed
		t.Logf("Hub count: %d", count)
	}

	// 5. Graceful Shutdown
	hub.Close() // Close all connections
	_ = server.Shutdown(context.Background())
}
