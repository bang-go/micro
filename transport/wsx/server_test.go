package wsx

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestServerDefaultOriginPolicy(t *testing.T) {
	t.Parallel()

	_, wsURL, originURL, _ := startTestWSServer(t, func(ctx context.Context, conn Connect) {
		_, _, _ = conn.ReadMessage(context.Background())
	})

	tests := []struct {
		name       string
		origin     string
		wantStatus int
	}{
		{name: "no origin allowed"},
		{name: "same origin allowed", origin: originURL},
		{name: "cross origin rejected", origin: "http://evil.example", wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &websocket.DialOptions{}
			if tt.origin != "" {
				opts.HTTPHeader = http.Header{"Origin": []string{tt.origin}}
			}

			conn, resp, err := websocket.Dial(context.Background(), wsURL, opts)
			if tt.wantStatus != 0 {
				if err == nil {
					_ = conn.Close(websocket.StatusNormalClosure, "unexpected success")
					t.Fatalf("expected dial to fail")
				}
				if resp == nil || resp.StatusCode != tt.wantStatus {
					got := 0
					if resp != nil {
						got = resp.StatusCode
					}
					t.Fatalf("unexpected status: got %d want %d", got, tt.wantStatus)
				}
				return
			}

			if err != nil {
				t.Fatalf("dial failed: %v", err)
			}
			_ = conn.Close(websocket.StatusNormalClosure, "done")
		})
	}
}

func TestServerShutdownClosesTrackedConnections(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	server := NewServer(&ServerConfig{
		Listener:        listener,
		ShutdownTimeout: time.Second,
	})

	var accepted sync.Once
	acceptedCh := make(chan struct{})
	startErrCh := make(chan error, 1)

	go func() {
		startErrCh <- server.Start(context.Background(), func(ctx context.Context, conn Connect) {
			accepted.Do(func() { close(acceptedCh) })
			_, _, _ = conn.ReadMessage(context.Background())
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws://"+listener.Addr().String()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	select {
	case <-acceptedCh:
	case <-time.After(time.Second):
		t.Fatal("handler did not accept connection")
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	readCtx, readCancel := context.WithTimeout(context.Background(), time.Second)
	defer readCancel()
	if _, _, err := conn.Read(readCtx); err == nil {
		t.Fatal("expected connection to be closed during shutdown")
	}

	select {
	case err := <-startErrCh:
		if err != nil {
			t.Fatalf("server returned unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not exit after shutdown")
	}
}

func TestServerStartStopsWhenContextCanceled(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	server := NewServer(&ServerConfig{
		Listener:        listener,
		ShutdownTimeout: time.Second,
	})

	startCtx, cancel := context.WithCancel(context.Background())
	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- server.Start(startCtx, func(ctx context.Context, conn Connect) {
			_, _, _ = conn.ReadMessage(context.Background())
		})
	}()

	conn, _, err := websocket.Dial(context.Background(), "ws://"+listener.Addr().String()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	cancel()

	select {
	case err := <-startErrCh:
		if err != nil {
			t.Fatalf("server returned unexpected error after context cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop after context cancel")
	}
}

func TestServerOnConnectRunsAfterHubRegister(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	hub, err := NewHub()
	if err != nil {
		t.Fatalf("new hub failed: %v", err)
	}
	defer hub.Close()

	joinedCh := make(chan struct{})
	server := NewServer(&ServerConfig{
		Listener:        listener,
		ShutdownTimeout: time.Second,
	},
		WithServerHub(hub),
		WithServerIdentify(func(context.Context, *http.Request) (string, error) {
			return "user-1", nil
		}),
		WithServerOnConnect(func(ctx context.Context, conn Connect, _ *http.Request) error {
			if err := hub.Join(ctx, conn.SessionID(), "room-1"); err != nil {
				return err
			}
			close(joinedCh)
			return nil
		}),
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), func(ctx context.Context, conn Connect) {
			_, _, _ = conn.ReadMessage(ctx)
		})
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

	conn, _, err := websocket.Dial(context.Background(), "ws://"+listener.Addr().String()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	select {
	case <-joinedCh:
	case <-time.After(time.Second):
		t.Fatal("onConnect did not join room")
	}
}

func TestServerOnDisconnectRunsBeforeHubUnregister(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	hub, err := NewHub()
	if err != nil {
		t.Fatalf("new hub failed: %v", err)
	}
	defer hub.Close()

	disconnectedCh := make(chan error, 1)
	server := NewServer(&ServerConfig{
		Listener:        listener,
		ShutdownTimeout: time.Second,
	},
		WithServerHub(hub),
		WithServerIdentify(func(context.Context, *http.Request) (string, error) {
			return "user-1", nil
		}),
		WithServerOnConnect(func(ctx context.Context, conn Connect, _ *http.Request) error {
			return hub.Join(ctx, conn.SessionID(), "room-1")
		}),
		WithServerOnDisconnect(func(ctx context.Context, conn Connect, _ *http.Request) {
			disconnectedCh <- hub.Leave(ctx, conn.SessionID(), "room-1")
		}),
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), func(ctx context.Context, conn Connect) {
			for {
				if _, _, err := conn.ReadMessage(ctx); err != nil {
					return
				}
			}
		})
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

	conn, _, err := websocket.Dial(context.Background(), "ws://"+listener.Addr().String()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "done")

	select {
	case err := <-disconnectedCh:
		if err != nil {
			t.Fatalf("onDisconnect could not access registered session: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("onDisconnect did not run")
	}
}
