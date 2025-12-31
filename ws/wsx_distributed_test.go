package ws

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// MockBroker in-memory for testing distributed logic without real Redis
type MockBroker struct {
	subscribers []func([]byte)
	mu          sync.Mutex
}

func (m *MockBroker) Subscribe(ctx context.Context, channel string, handler func(msg []byte)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers = append(m.subscribers, handler)
	return nil
}

func (m *MockBroker) Publish(ctx context.Context, channel string, msg []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Fan out to all subscribers (simulating multiple pods receiving)
	for _, handler := range m.subscribers {
		// Async to simulate network delay
		go handler(msg)
	}
	return nil
}

func (m *MockBroker) Close() error {
	return nil
}

func TestDistributedBroadcast(t *testing.T) {
	broker := &MockBroker{}

	// Simulate Pod A
	hubA := NewHub(WithHubBroker(broker))
	serverA := NewServer(&ServerConfig{Addr: "localhost:8891"})
	go serverA.Start(func(c Connect) {
		hubA.Register(c)
	})

	// Simulate Pod B
	hubB := NewHub(WithHubBroker(broker))
	serverB := NewServer(&ServerConfig{Addr: "localhost:8892"})
	go serverB.Start(func(c Connect) {
		hubB.Register(c)
	})

	time.Sleep(1 * time.Second)

	// Client 1 connects to Pod A
	client1 := NewClient("ws://localhost:8891/ws")
	msgCh1 := make(chan string, 1)
	client1.OnMessage(func(mt websocket.MessageType, msg []byte) {
		msgCh1 <- string(msg)
	})
	client1.Connect(context.Background())

	// Client 2 connects to Pod B
	client2 := NewClient("ws://localhost:8892/ws")
	msgCh2 := make(chan string, 1)
	client2.OnMessage(func(mt websocket.MessageType, msg []byte) {
		msgCh2 <- string(msg)
	})
	client2.Connect(context.Background())

	time.Sleep(1 * time.Second)

	// Action: Pod A broadcasts a message
	// Expected: Client 1 (on A) AND Client 2 (on B) receive it via Broker
	hubA.Broadcast([]byte("Hello Distributed World"))

	// Verify Client 1
	select {
	case msg := <-msgCh1:
		if msg != "Hello Distributed World" {
			t.Errorf("Client 1 got wrong message: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Error("Client 1 timeout")
	}

	// Verify Client 2
	select {
	case msg := <-msgCh2:
		if msg != "Hello Distributed World" {
			t.Errorf("Client 2 got wrong message: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Error("Client 2 timeout")
	}

	client1.Close()
	client2.Close()
	serverA.Shutdown(context.Background())
	serverB.Shutdown(context.Background())
}
