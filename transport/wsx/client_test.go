package wsx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestClientSingleStartAndIdempotentClose(t *testing.T) {
	t.Parallel()

	_, wsURL, _, _ := startTestWSServer(t, func(ctx context.Context, conn Connect) {
		_, _, _ = conn.ReadMessage(context.Background())
	})

	client := NewClient(
		wsURL,
		WithClientDialTimeout(200*time.Millisecond),
		WithClientReconnectInterval(20*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	secondCtx, secondCancel := context.WithTimeout(context.Background(), time.Second)
	defer secondCancel()
	if err := client.Start(secondCtx); !errors.Is(err, errClientAlreadyStarted) {
		t.Fatalf("unexpected second start error: %v", err)
	}

	client.Close()
	client.Close()
}

func TestClientMessageHookPanicTriggersDisconnectWithoutCrashing(t *testing.T) {
	t.Parallel()

	_, wsURL, _, _ := startTestWSServer(t, func(ctx context.Context, conn Connect) {
		_ = conn.SendText(context.Background(), "boom")
		_, _, _ = conn.ReadMessage(context.Background())
	})

	client := NewClient(
		wsURL,
		WithClientDialTimeout(200*time.Millisecond),
		WithClientReconnectInterval(20*time.Millisecond),
	)
	defer client.Close()

	disconnectCh := make(chan error, 1)
	client.OnMessage(func(context.Context, websocket.MessageType, []byte) {
		panic("handler boom")
	})
	client.OnDisconnect(func(ctx context.Context, err error) {
		select {
		case disconnectCh <- err:
		default:
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	select {
	case err := <-disconnectCh:
		if err == nil || !strings.Contains(err.Error(), "on_message") {
			t.Fatalf("unexpected disconnect error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected disconnect after message hook panic")
	}
}
