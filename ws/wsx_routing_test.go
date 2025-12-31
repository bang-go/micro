package ws

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDistributedRouting(t *testing.T) {
	broker := &MockBroker{}

	// Pod A
	hubA := NewHub(WithHubBroker(broker))
	serverA := NewServer(&ServerConfig{Addr: "localhost:8898"})
	go serverA.Start(func(c Connect) {
		// Identify user
		c.SetID("user-on-pod-a")
		hubA.Register(c)
		defer hubA.Unregister(c)
		// keep alive
		for {
			if _, _, err := c.ReadMessage(context.Background()); err != nil {
				return
			}
		}
	})

	// Pod B
	hubB := NewHub(WithHubBroker(broker))
	serverB := NewServer(&ServerConfig{Addr: "localhost:8899"})
	go serverB.Start(func(c Connect) {
		c.SetID("user-on-pod-b")
		hubB.Register(c)
		defer hubB.Unregister(c)
		for {
			if _, _, err := c.ReadMessage(context.Background()); err != nil {
				return
			}
		}
	})

	time.Sleep(1 * time.Second)

	// Clients
	clientA := NewClient("ws://localhost:8898/ws")
	msgChA := make(chan string, 1)
	clientA.OnMessage(func(mt websocket.MessageType, msg []byte) { msgChA <- string(msg) })
	clientA.Connect(context.Background())

	clientB := NewClient("ws://localhost:8899/ws")
	msgChB := make(chan string, 1)
	clientB.OnMessage(func(mt websocket.MessageType, msg []byte) { msgChB <- string(msg) })
	clientB.Connect(context.Background())

	time.Sleep(1 * time.Second)

	// 1. Test Distributed Unicast: Pod B sends to User on Pod A
	hubB.SendTo("user-on-pod-a", []byte("Direct Message"))

	select {
	case msg := <-msgChA:
		if msg != "Direct Message" {
			t.Errorf("Client A expected 'Direct Message', got %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Error("Client A timeout waiting for unicast")
	}

	// Verify Client B did NOT receive it
	select {
	case msg := <-msgChB:
		t.Errorf("Client B should NOT receive unicast, got %s", msg)
	default:
	}

	// 2. Test Distributed Broadcast: Pod A broadcasts
	hubA.Broadcast([]byte("Global Announcement"))

	select {
	case msg := <-msgChA:
		if msg != "Global Announcement" {
			t.Errorf("Client A wrong broadcast: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Error("Client A timeout broadcast")
	}

	select {
	case msg := <-msgChB:
		if msg != "Global Announcement" {
			t.Errorf("Client B wrong broadcast: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Error("Client B timeout broadcast")
	}

	clientA.Close()
	clientB.Close()
	serverA.Shutdown(context.Background())
	serverB.Shutdown(context.Background())
}
