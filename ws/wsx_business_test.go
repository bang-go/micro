package ws

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Mock RoomManager (Business Layer)
type RoomManager struct {
	rooms map[string]map[Connect]struct{}
	mu    sync.RWMutex
}

func NewRoomManager() *RoomManager {
	return &RoomManager{
		rooms: make(map[string]map[Connect]struct{}),
	}
}

func (rm *RoomManager) Join(room string, c Connect) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.rooms[room] == nil {
		rm.rooms[room] = make(map[Connect]struct{})
	}
	rm.rooms[room][c] = struct{}{}
}

func (rm *RoomManager) Broadcast(room string, msg []byte) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	for c := range rm.rooms[room] {
		c.SendBinary(context.Background(), msg)
	}
}

func TestBusinessLogic(t *testing.T) {
	// 1. Setup Server & Business Logic
	hub := NewHub()
	roomManager := NewRoomManager()

	server := NewServer(&ServerConfig{Addr: "localhost:8897"})

	go server.Start(func(c Connect) {
		hub.Register(c)
		defer hub.Unregister(c)

		// Simulate Business Logic: Read first message as "Subscribe"
		// In real app, this would be a JSON protocol
		mt, msg, err := c.ReadMessage(context.Background())
		if err != nil {
			return
		}

		if mt == websocket.MessageText {
			topic := string(msg) // e.g., "/user/123"

			// 2. Set Metadata on Connection
			c.SetID("client-session-1")
			c.Set("topic", topic)
			c.Set("role", "subscriber")

			// 3. Join Business Room
			roomManager.Join(topic, c)

			c.SendText(context.Background(), "Subscribed to "+topic)
		}

		// Keep alive
		for {
			_, _, err := c.ReadMessage(context.Background())
			if err != nil {
				return
			}
		}
	})

	time.Sleep(1 * time.Second)

	// 2. Client Connects & Subscribes
	client := NewClient("ws://localhost:8897/ws")
	msgCh := make(chan string, 10)

	client.OnConnect(func(c Connect) {
		// Send subscription
		c.SendText(context.Background(), "/user/123")
	})

	client.OnMessage(func(mt websocket.MessageType, msg []byte) {
		msgCh <- string(msg)
	})

	client.Connect(context.Background())

	// Wait for subscription ack
	select {
	case msg := <-msgCh:
		if msg != "Subscribed to /user/123" {
			t.Errorf("Unexpected ack: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for ack")
	}

	// 3. Business Broadcasts to Room
	roomManager.Broadcast("/user/123", []byte("Order Update"))

	select {
	case msg := <-msgCh:
		if msg != "Order Update" {
			t.Errorf("Expected Order Update, got %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for update")
	}

	client.Close()
	server.Shutdown(context.Background())
}
