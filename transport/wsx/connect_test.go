package wsx

import (
	"context"
	"errors"
	"testing"

	"github.com/coder/websocket"
)

func TestConnectSendCopiesPayloadBeforeQueueing(t *testing.T) {
	t.Parallel()

	conn := &connectEntity{
		sendChan:    make(chan message, 1),
		closed:      make(chan struct{}),
		roomSet:     make(map[string]struct{}),
		closeCtx:    context.Background(),
		closeCancel: func() {},
	}

	payload := []byte("hello")
	if err := conn.send(context.Background(), message{typ: websocket.MessageBinary, data: payload}); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	payload[0] = 'j'

	queued := <-conn.sendChan
	if got := string(queued.data); got != "hello" {
		t.Fatalf("unexpected queued payload: %q", got)
	}
}

func TestConnectSendAfterCloseReturnsError(t *testing.T) {
	t.Parallel()

	_, wsURL, _, _ := startTestWSServer(t, func(ctx context.Context, conn Connect) {
		_, _, _ = conn.ReadMessage(context.Background())
	})

	rawConn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	conn := NewConnect(rawConn, "127.0.0.1")
	if err := conn.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if err := conn.SendText(context.Background(), "hello"); !errors.Is(err, errConnectionClosed) {
		t.Fatalf("unexpected send error: %v", err)
	}
}

func TestConnectCloseAfterPeerNormalCloseIsSuccessfulAndIdempotent(t *testing.T) {
	t.Parallel()

	readErrCh := make(chan error, 1)
	closeErrCh := make(chan error, 1)
	_, wsURL, _, _ := startTestWSServer(t, func(ctx context.Context, conn Connect) {
		_, _, readErr := conn.ReadMessage(context.Background())
		readErrCh <- readErr
		firstCloseErr := conn.Close()
		secondCloseErr := conn.Close()
		closeErrCh <- errors.Join(firstCloseErr, secondCloseErr)
	})

	rawConn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	if err := rawConn.Close(websocket.StatusNormalClosure, "Client shutdown"); err != nil {
		t.Fatalf("peer close failed: %v", err)
	}

	if readErr := <-readErrCh; websocket.CloseStatus(readErr) != websocket.StatusNormalClosure {
		t.Fatalf("unexpected read close status: %v", readErr)
	}
	if closeErr := <-closeErrCh; closeErr != nil {
		t.Fatalf("connect close after peer normal close failed: %v", closeErr)
	}
}

func TestConnectCloseAfterPeerTransportEOFIsSuccessfulAndIdempotent(t *testing.T) {
	t.Parallel()

	readErrCh := make(chan error, 1)
	closeErrCh := make(chan error, 1)
	_, wsURL, _, _ := startTestWSServer(t, func(ctx context.Context, conn Connect) {
		_, _, readErr := conn.ReadMessage(context.Background())
		readErrCh <- readErr
		firstCloseErr := conn.Close()
		secondCloseErr := conn.Close()
		closeErrCh <- errors.Join(firstCloseErr, secondCloseErr)
	})

	rawConn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	if err := rawConn.CloseNow(); err != nil {
		t.Fatalf("peer immediate close failed: %v", err)
	}

	if readErr := <-readErrCh; readErr == nil {
		t.Fatal("expected peer transport close to terminate read")
	}
	if closeErr := <-closeErrCh; closeErr != nil {
		t.Fatalf("connect close after peer transport close failed: %v", closeErr)
	}
}
