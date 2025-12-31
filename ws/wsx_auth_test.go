package ws

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestAuthentication(t *testing.T) {
	// 1. Setup Server with Auth
	server := NewServer(&ServerConfig{Addr: "localhost:8890"},
		WithServerBeforeUpgrade(func(r *http.Request) error {
			token := r.URL.Query().Get("token")
			if token != "secret123" {
				return errors.New("invalid token")
			}
			return nil
		}),
		WithServerOnConnect(func(c Connect, r *http.Request) error {
			// Extract UserID from query or context
			// Simulate UserID binding
			c.SetID("user-888")
			return nil
		}),
	)

	hub := NewHub()

	go server.Start(func(c Connect) {
		hub.Register(c)
		defer hub.Unregister(c)

		// Echo ID
		c.SendText(context.Background(), "Welcome "+c.ID())

		for {
			if _, _, err := c.ReadMessage(context.Background()); err != nil {
				return
			}
		}
	})

	time.Sleep(1 * time.Second)

	// 2. Test Invalid Token
	clientBad := NewClient("ws://localhost:8890/ws?token=bad")
	err := clientBad.Connect(context.Background())
	if err == nil {
		t.Error("Expected error for invalid token, got nil")
	} else {
		t.Logf("Got expected error: %v", err)
	}

	// 3. Test Valid Token
	clientGood := NewClient("ws://localhost:8890/ws?token=secret123")
	msgCh := make(chan string, 1)
	clientGood.OnMessage(func(mt websocket.MessageType, msg []byte) {
		msgCh <- string(msg)
	})

	if err := clientGood.Connect(context.Background()); err != nil {
		t.Fatalf("Failed to connect with valid token: %v", err)
	}

	select {
	case msg := <-msgCh:
		if msg != "Welcome user-888" {
			t.Errorf("Expected welcome message with ID, got: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for welcome")
	}

	clientGood.Close()
	server.Shutdown(context.Background())
}
