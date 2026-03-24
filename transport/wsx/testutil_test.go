package wsx

import (
	"context"
	"net"
	"testing"
	"time"
)

func startTestWSServer(t *testing.T, handler func(context.Context, Connect)) (Server, string, string, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	server := NewServer(&ServerConfig{
		Listener:        listener,
		ShutdownTimeout: time.Second,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), handler)
	}()

	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("server returned unexpected error: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("server did not exit during cleanup")
		}
	})

	addr := listener.Addr().String()
	return server, "ws://" + addr + "/ws", "http://" + addr, errCh
}
