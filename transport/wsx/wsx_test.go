package wsx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/coder/websocket"
)

func TestHubUserRoomTargetsAllCurrentConnections(t *testing.T) {
	hub, err := NewHub(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := newStubConnect("app:user", "session-1")
	second := newStubConnect("app:user", "session-2")
	for _, conn := range []Connect{first, second} {
		if err := hub.Register(context.Background(), conn); err != nil {
			t.Fatal(err)
		}
	}
	if err := hub.JoinUserToRoom(context.Background(), "app:user", "room-1"); err != nil {
		t.Fatal(err)
	}
	if !first.hasRoom("room-1") || !second.hasRoom("room-1") {
		t.Fatal("all current user connections must join the room")
	}
	if err := hub.LeaveUserFromRoom(context.Background(), "app:user", "room-1"); err != nil {
		t.Fatal(err)
	}
	if first.hasRoom("room-1") || second.hasRoom("room-1") {
		t.Fatal("all current user connections must leave the room")
	}
}

func TestHubDisconnectWritesBeforeClose(t *testing.T) {
	hub, err := NewHub(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn := newStubConnect("app:user", "session-1")
	if err := hub.Register(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	if err := hub.DisconnectUser(context.Background(), "app:user", []byte("kick")); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(conn.operations(), ","); got != "write:kick,close" {
		t.Fatalf("operations = %q", got)
	}
}

func TestHubDistributedCommandUsesPublishRecipientCount(t *testing.T) {
	broker := newMemoryBroker()
	hub, err := NewHub(broker, WithHubNodeID("node-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn := newStubConnect("app:user", "session-1")
	if err := hub.Register(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	if err := hub.SendToUser(context.Background(), "app:user", []byte("message")); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(conn.operations(), ","); got != "write:message" {
		t.Fatalf("operations = %q", got)
	}
}

func TestEndpointPreparesOnceAndDrainsConnection(t *testing.T) {
	hub, err := NewHub(nil)
	if err != nil {
		t.Fatal(err)
	}
	var prepareCount atomic.Int32
	served := make(chan struct{})
	endpoint, err := NewEndpoint(EndpointConfig{Logger: logger.New()}, hub, func(context.Context, *http.Request) (Session, error) {
		prepareCount.Add(1)
		return sessionStub{userID: "app:user", serve: func(ctx context.Context, conn Connect) error {
			close(served)
			_, _, err := conn.ReadMessage(ctx)
			return err
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := endpoint.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(endpoint)
	defer server.Close()
	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-served:
	case <-time.After(time.Second):
		t.Fatal("connection session did not start")
	}
	if got := prepareCount.Load(); got != 1 {
		t.Fatalf("prepare count = %d", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := endpoint.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	_ = conn.CloseNow()
}

func TestConnectCloseNowClosesTransportAfterTermination(t *testing.T) {
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept() error = %v", err)
			return
		}
		accepted <- conn
	}))
	defer server.Close()

	client, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()
	raw := <-accepted
	conn, connectionCtx := newConnect(context.Background(), raw, "app:user", "127.0.0.1", 0, time.Second, true)

	conn.markTerminated()
	select {
	case <-connectionCtx.Done():
	default:
		t.Fatal("termination must cancel the connection context")
	}
	conn.mu.Lock()
	closedBefore := conn.closed
	conn.mu.Unlock()
	if closedBefore {
		t.Fatal("termination must not pretend the transport was closed")
	}
	if err := conn.CloseNow(); err != nil {
		t.Fatal(err)
	}
	conn.mu.Lock()
	closedAfter := conn.closed
	conn.mu.Unlock()
	if !closedAfter {
		t.Fatal("CloseNow must close a previously terminated transport")
	}
	readCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := client.Read(readCtx); err == nil {
		t.Fatal("client read error = nil, want closed transport")
	}
}

func TestHubCommandTimeoutCapsLongerCallerDeadline(t *testing.T) {
	broker := &unresponsiveBroker{}
	hub, err := NewHub(broker, WithHubCommandTimeout(20*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	err = hub.SendToUser(ctx, "app:user", []byte("message"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SendToUser() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("command timeout took %s, want hub timeout", elapsed)
	}
}

func TestPublicContextContractsRejectNil(t *testing.T) {
	hub, err := NewHub(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.start(nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("start error = %v", err)
	}
}

func TestRedisBrokerRejectsNilClient(t *testing.T) {
	if _, err := NewRedisBrokerWithClient(nil); !errors.Is(err, ErrBrokerClientRequired) {
		t.Fatalf("error = %v", err)
	}
}

type sessionStub struct {
	userID string
	serve  func(context.Context, Connect) error
}

func (s sessionStub) UserID() string                                { return s.userID }
func (s sessionStub) Serve(ctx context.Context, conn Connect) error { return s.serve(ctx, conn) }

type stubConnect struct {
	userID    string
	sessionID string
	mu        sync.Mutex
	roomSet   map[string]struct{}
	ops       []string
}

func newStubConnect(userID, sessionID string) *stubConnect {
	return &stubConnect{userID: userID, sessionID: sessionID, roomSet: make(map[string]struct{})}
}

func (c *stubConnect) WriteMessage(_ context.Context, _ websocket.MessageType, data []byte) error {
	c.mu.Lock()
	c.ops = append(c.ops, "write:"+string(data))
	c.mu.Unlock()
	return nil
}
func (c *stubConnect) ReadMessage(context.Context) (websocket.MessageType, []byte, error) {
	return 0, nil, ErrConnectionClosed
}
func (c *stubConnect) CloseNow() error {
	c.mu.Lock()
	c.ops = append(c.ops, "close")
	c.mu.Unlock()
	return nil
}
func (c *stubConnect) RemoteAddr() string { return "127.0.0.1" }
func (c *stubConnect) UserID() string     { return c.userID }
func (c *stubConnect) SessionID() string  { return c.sessionID }
func (c *stubConnect) roomCount() int     { c.mu.Lock(); defer c.mu.Unlock(); return len(c.roomSet) }
func (c *stubConnect) rooms() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.roomSet))
	for room := range c.roomSet {
		out = append(out, room)
	}
	return out
}
func (c *stubConnect) joinRoom(room string)  { c.mu.Lock(); c.roomSet[room] = struct{}{}; c.mu.Unlock() }
func (c *stubConnect) leaveRoom(room string) { c.mu.Lock(); delete(c.roomSet, room); c.mu.Unlock() }
func (c *stubConnect) hasRoom(room string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.roomSet[room]
	return ok
}
func (c *stubConnect) operations() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.ops...)
}

type memoryBroker struct {
	mu       sync.Mutex
	nextID   uint64
	handlers map[string]map[uint64]func([]byte)
	closed   bool
}

func newMemoryBroker() *memoryBroker {
	return &memoryBroker{handlers: make(map[string]map[uint64]func([]byte))}
}

func (b *memoryBroker) Subscribe(ctx context.Context, channel string, handler func([]byte)) (Subscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrBrokerClosed
	}
	id := b.nextID
	b.nextID++
	if b.handlers[channel] == nil {
		b.handlers[channel] = make(map[uint64]func([]byte))
	}
	b.handlers[channel][id] = handler
	return &memorySubscription{broker: b, channel: channel, id: id}, nil
}

func (b *memoryBroker) Publish(ctx context.Context, channel string, data []byte) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	b.mu.Lock()
	handlers := make([]func([]byte), 0, len(b.handlers[channel]))
	for _, handler := range b.handlers[channel] {
		handlers = append(handlers, handler)
	}
	b.mu.Unlock()
	for _, handler := range handlers {
		handler(append([]byte(nil), data...))
	}
	return int64(len(handlers)), nil
}

func (b *memoryBroker) Shutdown(context.Context) error {
	b.mu.Lock()
	b.closed = true
	b.handlers = make(map[string]map[uint64]func([]byte))
	b.mu.Unlock()
	return nil
}

type memorySubscription struct {
	broker  *memoryBroker
	channel string
	id      uint64
}

func (s *memorySubscription) Close(context.Context) error {
	s.broker.mu.Lock()
	delete(s.broker.handlers[s.channel], s.id)
	s.broker.mu.Unlock()
	return nil
}

type unresponsiveBroker struct{}

func (*unresponsiveBroker) Subscribe(context.Context, string, func([]byte)) (Subscription, error) {
	return noopSubscription{}, nil
}

func (*unresponsiveBroker) Publish(context.Context, string, []byte) (int64, error) {
	return 1, nil
}

func (*unresponsiveBroker) Shutdown(context.Context) error { return nil }

type noopSubscription struct{}

func (noopSubscription) Close(context.Context) error { return nil }
