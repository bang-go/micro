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

func TestClientReconnectBackoffIsBounded(t *testing.T) {
	t.Parallel()

	client := NewClient(
		"ws://127.0.0.1/ws",
		WithClientReconnectInterval(time.Millisecond),
	).(*clientEntity)

	for _, attempt := range []int{1, 8, 64, 1024} {
		delay := client.calculateBackoff(attempt)
		if delay <= 0 {
			t.Fatalf("attempt %d produced non-positive delay: %s", attempt, delay)
		}
		if delay > 30*time.Second {
			t.Fatalf("attempt %d exceeded max delay: %s", attempt, delay)
		}
	}
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

func TestClientDisconnectHookPanicIsReturnedOnClose(t *testing.T) {
	t.Parallel()

	_, wsURL, _, _ := startTestWSServer(t, func(ctx context.Context, conn Connect) {
		_ = conn.SendText(context.Background(), "close")
		_ = conn.Close()
	})

	client := NewClient(
		wsURL,
		WithClientDialTimeout(200*time.Millisecond),
		WithClientReconnectInterval(20*time.Millisecond),
		WithClientMaxReconnectAttempts(0),
	).(*clientEntity)
	client.OnDisconnect(func(context.Context, error) {
		panic("disconnect boom")
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := client.currentCloseErr(); err != nil && strings.Contains(err.Error(), "on_disconnect") {
			if closeErr := client.Close(); closeErr == nil || !strings.Contains(closeErr.Error(), "on_disconnect") {
				t.Fatalf("expected disconnect hook error on close, got %v", closeErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("disconnect hook panic was not recorded")
}
