package ws

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWSX(t *testing.T) {
	addr := "localhost:8889"

	// 1. Start Server
	serverConf := &ServerConfig{Addr: addr}
	server := NewServer(serverConf)

	go func() {
		err := server.Start(func(conn Connect) {
			fmt.Println("[Server] New Connection")
			go func() {
				for {
					mt, msg, err := conn.ReadMessage(context.Background())
					if err != nil {
						fmt.Printf("[Server] Read Error: %v\n", err)
						return
					}
					fmt.Printf("[Server] Received: %s\n", string(msg))

					// Echo back
					if mt == websocket.MessageText {
						if err := conn.SendText(context.Background(), "Echo: "+string(msg)); err != nil {
							fmt.Printf("[Server] Send Error: %v\n", err)
							return
						}
					}
				}
			}()
		})
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("Server start failed: %v", err)
		}
	}()

	time.Sleep(1 * time.Second) // Wait for server start

	// 2. Start Client
	client := NewClient("ws://" + addr + "/ws")

	msgCount := 0
	client.OnConnect(func(c Connect) {
		fmt.Println("[Client] Connected")
		c.SendText(context.Background(), "Hello World")
	})

	client.OnMessage(func(mt websocket.MessageType, data []byte) {
		fmt.Printf("[Client] Received: %s\n", string(data))
		msgCount++
	})

	client.OnClose(func(err error) {
		fmt.Printf("[Client] Closed: %v\n", err)
	})

	client.Connect(context.Background())

	time.Sleep(2 * time.Second)

	if msgCount == 0 {
		t.Error("Client should receive echo message")
	}

	// 3. Test Reconnect (Simulate by closing server? Hard to do cleanly in one process without restarting server on same port,
	//    but we can test client close and reconnect logic if we had a way to drop connection)

	client.Close()
	_ = server.Shutdown(context.Background())
}
