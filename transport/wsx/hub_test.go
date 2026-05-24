package wsx

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestHubLocalOperations(t *testing.T) {
	t.Parallel()

	hub, err := NewHub(
		WithHubSendTimeout(time.Second),
		WithHubMaxConcurrentSends(4),
	)
	if err != nil {
		t.Fatalf("new hub failed: %v", err)
	}

	user1 := newStubConnect("user-1", "session-1")
	user2 := newStubConnect("user-2", "session-2")

	if err := hub.Register(user1); err != nil {
		t.Fatalf("register user1 failed: %v", err)
	}
	if err := hub.Register(user2); err != nil {
		t.Fatalf("register user2 failed: %v", err)
	}

	if err := hub.Broadcast(context.Background(), []byte("broadcast")); err != nil {
		t.Fatalf("broadcast failed: %v", err)
	}
	if got := user1.Messages(); len(got) != 1 || string(got[0]) != "broadcast" {
		t.Fatalf("unexpected user1 broadcast payload: %q", got)
	}
	if got := user2.Messages(); len(got) != 1 || string(got[0]) != "broadcast" {
		t.Fatalf("unexpected user2 broadcast payload: %q", got)
	}

	if err := hub.Join(context.Background(), user1.SessionID(), "room-1"); err != nil {
		t.Fatalf("join failed: %v", err)
	}
	if err := hub.BroadcastToRoom(context.Background(), "room-1", []byte("room")); err != nil {
		t.Fatalf("broadcast to room failed: %v", err)
	}
	if got := user1.Messages(); len(got) != 2 || string(got[1]) != "room" {
		t.Fatalf("unexpected user1 room payload: %q", got)
	}
	if got := user2.Messages(); len(got) != 1 {
		t.Fatalf("user2 should not receive room payload: %q", got)
	}

	if err := hub.SendTo(context.Background(), "user-2", []byte("direct")); err != nil {
		t.Fatalf("send to failed: %v", err)
	}
	if got := user2.Messages(); len(got) != 2 || string(got[1]) != "direct" {
		t.Fatalf("unexpected user2 direct payload: %q", got)
	}

	if err := hub.Kick(context.Background(), "user-1"); err != nil {
		t.Fatalf("kick failed: %v", err)
	}
	if got := user1.CloseCount(); got != 1 {
		t.Fatalf("unexpected user1 close count: %d", got)
	}

	if err := hub.SendJSONTo(context.Background(), "user-2", map[string]string{"kind": "json"}); err != nil {
		t.Fatalf("send json failed: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(user2.Messages()[2], &payload); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}
	if payload["kind"] != "json" {
		t.Fatalf("unexpected json payload: %+v", payload)
	}

	hub.Close()
	if got := user2.CloseCount(); got != 1 {
		t.Fatalf("unexpected user2 close count after hub close: %d", got)
	}
}

func TestHubRegisterDoesNotKickExistingUserConnections(t *testing.T) {
	t.Parallel()

	hub, err := NewHub()
	if err != nil {
		t.Fatalf("new hub failed: %v", err)
	}
	defer hub.Close()

	first := newStubConnect("user-1", "session-1")
	second := newStubConnect("user-1", "session-2")

	if err := hub.Register(first); err != nil {
		t.Fatalf("register first failed: %v", err)
	}
	if err := hub.Register(second); err != nil {
		t.Fatalf("register second failed: %v", err)
	}

	if got := first.CloseCount(); got != 0 {
		t.Fatalf("first connection was kicked during register: %d", got)
	}
	if got := second.CloseCount(); got != 0 {
		t.Fatalf("second connection was closed during register: %d", got)
	}

	if err := hub.SendTo(context.Background(), "user-1", []byte("direct")); err != nil {
		t.Fatalf("send to user failed: %v", err)
	}
	if got := first.Messages(); len(got) != 1 || string(got[0]) != "direct" {
		t.Fatalf("unexpected first messages: %q", got)
	}
	if got := second.Messages(); len(got) != 1 || string(got[0]) != "direct" {
		t.Fatalf("unexpected second messages: %q", got)
	}
}

func TestHubRegisterValidatesConnectionAndSession(t *testing.T) {
	t.Parallel()

	hub, err := NewHub()
	if err != nil {
		t.Fatalf("new hub failed: %v", err)
	}
	defer hub.Close()

	if err := hub.Register(nil); !errors.Is(err, errHubConnectionMissing) {
		t.Fatalf("expected missing connection error, got %v", err)
	}

	blankSession := newStubConnect("user-1", "")
	if err := hub.Register(blankSession); !errors.Is(err, errHubSessionIDMissing) {
		t.Fatalf("expected missing session error, got %v", err)
	}

	first := newStubConnect("user-1", "session-1")
	if err := hub.Register(first); err != nil {
		t.Fatalf("register first failed: %v", err)
	}
	if err := hub.Register(first); !errors.Is(err, errHubSessionDuplicate) {
		t.Fatalf("expected duplicate registration error, got %v", err)
	}

	duplicate := newStubConnect("user-2", "session-1")
	if err := hub.Register(duplicate); !errors.Is(err, errHubSessionDuplicate) {
		t.Fatalf("expected duplicate session error, got %v", err)
	}
}

func TestHubSessionOperationsTargetOnlyOneConnection(t *testing.T) {
	t.Parallel()

	hub, err := NewHub()
	if err != nil {
		t.Fatalf("new hub failed: %v", err)
	}
	defer hub.Close()

	first := newStubConnect("user-1", "session-1")
	second := newStubConnect("user-1", "session-2")
	other := newStubConnect("user-2", "session-3")
	for _, conn := range []*stubConnect{first, second, other} {
		if err := hub.Register(conn); err != nil {
			t.Fatalf("register failed: %v", err)
		}
	}

	if err := hub.SendToSession(context.Background(), "session-2", []byte("session-direct")); err != nil {
		t.Fatalf("send to session failed: %v", err)
	}
	if got := first.Messages(); len(got) != 0 {
		t.Fatalf("first session should not receive payload: %q", got)
	}
	if got := second.Messages(); len(got) != 1 || string(got[0]) != "session-direct" {
		t.Fatalf("unexpected second session payload: %q", got)
	}
	if got := other.Messages(); len(got) != 0 {
		t.Fatalf("other user should not receive payload: %q", got)
	}

	if err := hub.KickSession(context.Background(), "session-2"); err != nil {
		t.Fatalf("kick session failed: %v", err)
	}
	if got := first.CloseCount(); got != 0 {
		t.Fatalf("first session should not be kicked: %d", got)
	}
	if got := second.CloseCount(); got != 1 {
		t.Fatalf("second session should be kicked once: %d", got)
	}
	if got := other.CloseCount(); got != 0 {
		t.Fatalf("other user should not be kicked: %d", got)
	}
}

func TestHubKickRoomLocalClosesOnlyRoomMembers(t *testing.T) {
	t.Parallel()

	hub, err := NewHub()
	if err != nil {
		t.Fatalf("new hub failed: %v", err)
	}
	defer hub.Close()

	roomMember := newStubConnect("user-1", "session-1")
	other := newStubConnect("user-2", "session-2")
	if err := hub.Register(roomMember); err != nil {
		t.Fatalf("register room member failed: %v", err)
	}
	if err := hub.Register(other); err != nil {
		t.Fatalf("register other failed: %v", err)
	}
	if err := hub.Join(context.Background(), roomMember.SessionID(), "room-1"); err != nil {
		t.Fatalf("join failed: %v", err)
	}

	if err := hub.KickRoom(context.Background(), "room-1"); err != nil {
		t.Fatalf("kick room failed: %v", err)
	}
	if got := roomMember.CloseCount(); got != 1 {
		t.Fatalf("room member should be kicked once: %d", got)
	}
	if got := other.CloseCount(); got != 0 {
		t.Fatalf("non-room member should not be kicked: %d", got)
	}
}

func TestHubKickRoomReturnsCloseErrors(t *testing.T) {
	t.Parallel()

	hub, err := NewHub()
	if err != nil {
		t.Fatalf("new hub failed: %v", err)
	}
	defer hub.Close()

	closeErr := errors.New("close failed")
	conn := newStubConnect("user-1", "session-1")
	conn.closeErr = closeErr
	if err := hub.Register(conn); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if err := hub.Join(context.Background(), conn.SessionID(), "room-1"); err != nil {
		t.Fatalf("join failed: %v", err)
	}

	if err := hub.KickRoom(context.Background(), "room-1"); !errors.Is(err, closeErr) {
		t.Fatalf("expected close error, got %v", err)
	}
	if got := conn.CloseCount(); got != 1 {
		t.Fatalf("connection should still be closed once: %d", got)
	}
}

func TestHubDistributedOperationsWaitForAck(t *testing.T) {
	t.Parallel()

	broker := newMemoryBroker()

	hub1, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:test"),
		WithHubNodeID("node-1"),
	)
	if err != nil {
		t.Fatalf("new hub1 failed: %v", err)
	}
	defer hub1.Close()

	hub2, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:test"),
		WithHubNodeID("node-2"),
	)
	if err != nil {
		t.Fatalf("new hub2 failed: %v", err)
	}
	defer hub2.Close()

	user1 := newStubConnect("user-1", "session-1")
	user2 := newStubConnect("user-2", "session-2")
	if err := hub1.Register(user1); err != nil {
		t.Fatalf("register user1 failed: %v", err)
	}
	if err := hub2.Register(user2); err != nil {
		t.Fatalf("register user2 failed: %v", err)
	}

	if err := hub1.Broadcast(context.Background(), []byte("cluster-broadcast")); err != nil {
		t.Fatalf("cluster broadcast failed: %v", err)
	}
	if got := user1.Messages(); len(got) != 1 || string(got[0]) != "cluster-broadcast" {
		t.Fatalf("unexpected user1 cluster payload: %q", got)
	}
	if got := user2.Messages(); len(got) != 1 || string(got[0]) != "cluster-broadcast" {
		t.Fatalf("unexpected user2 cluster payload: %q", got)
	}

	if err := hub1.SendTo(context.Background(), "user-2", []byte("cluster-direct")); err != nil {
		t.Fatalf("cluster direct send failed: %v", err)
	}
	if got := user2.Messages(); len(got) != 2 || string(got[1]) != "cluster-direct" {
		t.Fatalf("unexpected user2 direct payload: %q", got)
	}
}

func TestHubDistributedOperationsRequireAckBeforeReturn(t *testing.T) {
	t.Parallel()

	broker := newBlockingBroker()

	hub, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:blocking"),
		WithHubNodeID("node-1"),
		WithHubCommandTimeout(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("new hub failed: %v", err)
	}
	defer hub.Close()

	broker.addSubscriber("ws:blocking")
	if err := hub.Broadcast(context.Background(), []byte("blocked")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected command timeout, got %v", err)
	}
}

func TestHubDistributedUserOperationsRouteByUserSubscription(t *testing.T) {
	t.Parallel()

	broker := newMemoryBroker()

	hub1, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:user"),
		WithHubNodeID("node-1"),
	)
	if err != nil {
		t.Fatalf("new hub1 failed: %v", err)
	}
	defer hub1.Close()

	hub2, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:user"),
		WithHubNodeID("node-2"),
	)
	if err != nil {
		t.Fatalf("new hub2 failed: %v", err)
	}
	defer hub2.Close()

	shared1 := newStubConnect("shared", "session-1")
	isolated := newStubConnect("isolated", "session-2")
	shared2 := newStubConnect("shared", "session-3")

	if err := hub1.Register(shared1); err != nil {
		t.Fatalf("register shared1 failed: %v", err)
	}
	if err := hub1.Register(isolated); err != nil {
		t.Fatalf("register isolated failed: %v", err)
	}
	if err := hub2.Register(shared2); err != nil {
		t.Fatalf("register shared2 failed: %v", err)
	}

	sharedChannel := hub1.(*hubEntity).userChannel("shared")
	isolatedChannel := hub1.(*hubEntity).userChannel("isolated")
	waitForSubscribers(t, broker, sharedChannel, 2)
	waitForSubscribers(t, broker, isolatedChannel, 1)

	if err := hub1.SendTo(context.Background(), "shared", []byte("private")); err != nil {
		t.Fatalf("send to shared failed: %v", err)
	}
	if got := shared1.Messages(); len(got) != 1 || string(got[0]) != "private" {
		t.Fatalf("unexpected shared1 payload: %q", got)
	}
	if got := shared2.Messages(); len(got) != 1 || string(got[0]) != "private" {
		t.Fatalf("unexpected shared2 payload: %q", got)
	}
	if got := isolated.Messages(); len(got) != 0 {
		t.Fatalf("isolated user should not receive shared payload: %q", got)
	}

	if err := hub1.KickWithOrigin(context.Background(), "shared", shared1.SessionID()); err != nil {
		t.Fatalf("kick with origin failed: %v", err)
	}
	if got := shared1.CloseCount(); got != 0 {
		t.Fatalf("origin session should not be kicked: %d", got)
	}
	if got := shared2.CloseCount(); got != 1 {
		t.Fatalf("remote shared session should be kicked once: %d", got)
	}
	if got := isolated.CloseCount(); got != 0 {
		t.Fatalf("isolated user should not be kicked: %d", got)
	}
}

func TestHubDistributedUserOperationsReturnRemoteErrors(t *testing.T) {
	t.Parallel()

	broker := newMemoryBroker()

	hub1, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:user-error"),
		WithHubNodeID("node-1"),
	)
	if err != nil {
		t.Fatalf("new hub1 failed: %v", err)
	}
	defer hub1.Close()

	hub2, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:user-error"),
		WithHubNodeID("node-2"),
	)
	if err != nil {
		t.Fatalf("new hub2 failed: %v", err)
	}
	defer hub2.Close()

	closeErr := errors.New("remote close failed")
	conn := newStubConnect("shared", "session-1")
	conn.closeErr = closeErr
	if err := hub2.Register(conn); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	waitForSubscribers(t, broker, hub1.(*hubEntity).userChannel("shared"), 1)
	if err := hub1.Kick(context.Background(), "shared"); err == nil || !strings.Contains(err.Error(), closeErr.Error()) {
		t.Fatalf("expected remote close error, got %v", err)
	}
	if got := conn.CloseCount(); got != 1 {
		t.Fatalf("remote connection should be closed once: %d", got)
	}
}

func TestHubDistributedSessionOperationsTargetOnlyOneConnection(t *testing.T) {
	t.Parallel()

	broker := newMemoryBroker()

	hub1, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:session"),
		WithHubNodeID("node-1"),
	)
	if err != nil {
		t.Fatalf("new hub1 failed: %v", err)
	}
	defer hub1.Close()

	hub2, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:session"),
		WithHubNodeID("node-2"),
	)
	if err != nil {
		t.Fatalf("new hub2 failed: %v", err)
	}
	defer hub2.Close()

	local := newStubConnect("shared", "session-1")
	remote := newStubConnect("shared", "session-2")
	if err := hub1.Register(local); err != nil {
		t.Fatalf("register local failed: %v", err)
	}
	if err := hub2.Register(remote); err != nil {
		t.Fatalf("register remote failed: %v", err)
	}
	waitForSubscribers(t, broker, hub2.(*hubEntity).sessionChannel("session-2"), 1)

	if err := hub1.SendToSession(context.Background(), "session-2", []byte("remote-session")); err != nil {
		t.Fatalf("send to remote session failed: %v", err)
	}
	if got := local.Messages(); len(got) != 0 {
		t.Fatalf("local session should not receive payload: %q", got)
	}
	if got := remote.Messages(); len(got) != 1 || string(got[0]) != "remote-session" {
		t.Fatalf("unexpected remote session payload: %q", got)
	}

	if err := hub1.KickSession(context.Background(), "session-2"); err != nil {
		t.Fatalf("kick remote session failed: %v", err)
	}
	if got := local.CloseCount(); got != 0 {
		t.Fatalf("local session should not be kicked: %d", got)
	}
	if got := remote.CloseCount(); got != 1 {
		t.Fatalf("remote session should be kicked once: %d", got)
	}
}

func TestHubDistributedRoomOperationsRouteByRoomSubscription(t *testing.T) {
	t.Parallel()

	broker := newMemoryBroker()

	hub1, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:room"),
		WithHubNodeID("node-1"),
	)
	if err != nil {
		t.Fatalf("new hub1 failed: %v", err)
	}
	defer hub1.Close()

	hub2, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:room"),
		WithHubNodeID("node-2"),
	)
	if err != nil {
		t.Fatalf("new hub2 failed: %v", err)
	}
	defer hub2.Close()

	user1 := newStubConnect("user-1", "session-1")
	user2 := newStubConnect("user-1", "session-2")
	if err := hub1.Register(user1); err != nil {
		t.Fatalf("register user1 failed: %v", err)
	}
	if err := hub2.Register(user2); err != nil {
		t.Fatalf("register user2 failed: %v", err)
	}

	roomChannel := hub1.(*hubEntity).roomChannel("room-1")

	if err := hub1.Join(context.Background(), user1.SessionID(), "room-1"); err != nil {
		t.Fatalf("hub1 join failed: %v", err)
	}
	waitForSubscribers(t, broker, roomChannel, 1)

	if err := hub2.BroadcastToRoom(context.Background(), "room-1", []byte("room-1")); err != nil {
		t.Fatalf("cluster room broadcast failed: %v", err)
	}
	if got := user1.Messages(); len(got) != 1 || string(got[0]) != "room-1" {
		t.Fatalf("unexpected user1 room payload: %q", got)
	}
	if got := user2.Messages(); len(got) != 0 {
		t.Fatalf("session-scoped join should not leak to user2: %q", got)
	}

	if err := hub2.Join(context.Background(), user2.SessionID(), "room-1"); err != nil {
		t.Fatalf("hub2 join failed: %v", err)
	}
	waitForSubscribers(t, broker, roomChannel, 2)

	if err := hub1.BroadcastToRoom(context.Background(), "room-1", []byte("room-2")); err != nil {
		t.Fatalf("second cluster room broadcast failed: %v", err)
	}
	if got := user1.Messages(); len(got) != 2 || string(got[1]) != "room-2" {
		t.Fatalf("unexpected user1 second room payload: %q", got)
	}
	if got := user2.Messages(); len(got) != 1 || string(got[0]) != "room-2" {
		t.Fatalf("unexpected user2 room payload after join: %q", got)
	}

	if err := hub2.Leave(context.Background(), user2.SessionID(), "room-1"); err != nil {
		t.Fatalf("hub2 leave failed: %v", err)
	}
	waitForSubscribers(t, broker, roomChannel, 1)

	if err := hub1.BroadcastToRoom(context.Background(), "room-1", []byte("room-3")); err != nil {
		t.Fatalf("third cluster room broadcast failed: %v", err)
	}
	if got := user1.Messages(); len(got) != 3 || string(got[2]) != "room-3" {
		t.Fatalf("unexpected user1 third room payload: %q", got)
	}
	if got := user2.Messages(); len(got) != 1 {
		t.Fatalf("user2 should stop receiving room payload after leave: %q", got)
	}

	if err := hub2.KickRoom(context.Background(), "room-1"); err != nil {
		t.Fatalf("cluster room kick failed: %v", err)
	}
	if got := user1.CloseCount(); got != 1 {
		t.Fatalf("remaining room member should be kicked once: %d", got)
	}
	if got := user2.CloseCount(); got != 0 {
		t.Fatalf("left session should not be kicked: %d", got)
	}
}

func TestHubDistributedRoomOperationsReturnRemoteErrors(t *testing.T) {
	t.Parallel()

	broker := newMemoryBroker()

	hub1, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:room-error"),
		WithHubNodeID("node-1"),
	)
	if err != nil {
		t.Fatalf("new hub1 failed: %v", err)
	}
	defer hub1.Close()

	hub2, err := NewHub(
		WithHubBroker(broker),
		WithHubChannel("ws:room-error"),
		WithHubNodeID("node-2"),
	)
	if err != nil {
		t.Fatalf("new hub2 failed: %v", err)
	}
	defer hub2.Close()

	closeErr := errors.New("remote room close failed")
	conn := newStubConnect("user-1", "session-1")
	conn.closeErr = closeErr
	if err := hub2.Register(conn); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if err := hub2.Join(context.Background(), conn.SessionID(), "room-1"); err != nil {
		t.Fatalf("join failed: %v", err)
	}

	waitForSubscribers(t, broker, hub1.(*hubEntity).roomChannel("room-1"), 1)
	if err := hub1.KickRoom(context.Background(), "room-1"); err == nil || !strings.Contains(err.Error(), closeErr.Error()) {
		t.Fatalf("expected remote room close error, got %v", err)
	}
	if got := conn.CloseCount(); got != 1 {
		t.Fatalf("remote room connection should be closed once: %d", got)
	}
}

func TestHubRoomOperationsValidateInputs(t *testing.T) {
	t.Parallel()

	hub, err := NewHub()
	if err != nil {
		t.Fatalf("new hub failed: %v", err)
	}
	defer hub.Close()

	if err := hub.BroadcastToRoom(context.Background(), "", []byte("msg")); !errors.Is(err, errHubRoomMissing) {
		t.Fatalf("expected room missing error, got %v", err)
	}
	if err := hub.KickRoom(context.Background(), ""); !errors.Is(err, errHubRoomMissing) {
		t.Fatalf("expected room missing error on kick room, got %v", err)
	}
	if err := hub.Join(context.Background(), "", "room-1"); !errors.Is(err, errHubSessionIDMissing) {
		t.Fatalf("expected session id missing error, got %v", err)
	}
	if err := hub.Join(context.Background(), "missing-session", "room-1"); !errors.Is(err, errHubSessionNotFound) {
		t.Fatalf("expected session not found error, got %v", err)
	}
	if err := hub.Leave(context.Background(), "", "room-1"); !errors.Is(err, errHubSessionIDMissing) {
		t.Fatalf("expected session id missing error on leave, got %v", err)
	}
}

type stubConnect struct {
	userID    string
	sessionID string

	mu         sync.Mutex
	messages   [][]byte
	roomSet    map[string]struct{}
	closeCount int
	closeErr   error
}

func newStubConnect(userID string, sessionID string) *stubConnect {
	return &stubConnect{
		userID:    userID,
		sessionID: sessionID,
		roomSet:   make(map[string]struct{}),
	}
}

func (c *stubConnect) SendText(ctx context.Context, text string) error {
	return c.SendBinary(ctx, []byte(text))
}

func (c *stubConnect) SendBinary(ctx context.Context, data []byte) error {
	select {
	case <-normalizeContext(ctx).Done():
		return ctx.Err()
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cloned := append([]byte(nil), data...)
	c.messages = append(c.messages, cloned)
	return nil
}

func (c *stubConnect) SendJSON(ctx context.Context, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.SendBinary(ctx, data)
}

func (c *stubConnect) ReadMessage(context.Context) (websocket.MessageType, []byte, error) {
	return 0, nil, errors.New("not implemented")
}

func (c *stubConnect) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeCount++
	return c.closeErr
}

func (c *stubConnect) UserID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.userID
}

func (c *stubConnect) RemoteAddr() string {
	return "127.0.0.1"
}

func (c *stubConnect) SessionID() string {
	return c.sessionID
}

func (c *stubConnect) rooms() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	rooms := make([]string, 0, len(c.roomSet))
	for room := range c.roomSet {
		rooms = append(rooms, room)
	}
	return rooms
}

func (c *stubConnect) roomCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.roomSet)
}

func (c *stubConnect) joinRoom(room string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roomSet[room] = struct{}{}
}

func (c *stubConnect) leaveRoom(room string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.roomSet, room)
}

func (c *stubConnect) Messages() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	msgs := make([][]byte, 0, len(c.messages))
	for _, msg := range c.messages {
		msgs = append(msgs, append([]byte(nil), msg...))
	}
	return msgs
}

func (c *stubConnect) CloseCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCount
}

func waitForSubscribers(t *testing.T, broker *memoryBroker, channel string, want int64) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, err := broker.NumSubscribers(context.Background(), channel)
		if err == nil && got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	got, err := broker.NumSubscribers(context.Background(), channel)
	t.Fatalf("unexpected subscriber count for %s: got=%d want=%d err=%v", channel, got, want, err)
}

type memoryBroker struct {
	mu       sync.RWMutex
	nextID   uint64
	closed   bool
	handlers map[string]map[uint64]func([]byte)
}

func newMemoryBroker() *memoryBroker {
	return &memoryBroker{
		handlers: make(map[string]map[uint64]func([]byte)),
	}
}

func (b *memoryBroker) Subscribe(ctx context.Context, channel string, handler func([]byte)) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errBrokerClosed
	}
	if b.handlers[channel] == nil {
		b.handlers[channel] = make(map[uint64]func([]byte))
	}
	id := b.nextID
	b.nextID++
	b.handlers[channel][id] = handler
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		defer b.mu.Unlock()
		if handlers := b.handlers[channel]; handlers != nil {
			delete(handlers, id)
			if len(handlers) == 0 {
				delete(b.handlers, channel)
			}
		}
	}()

	return nil
}

func (b *memoryBroker) Publish(ctx context.Context, channel string, msg []byte) error {
	if err := normalizeContext(ctx).Err(); err != nil {
		return err
	}

	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return errBrokerClosed
	}
	handlers := make([]func([]byte), 0, len(b.handlers[channel]))
	for _, handler := range b.handlers[channel] {
		handlers = append(handlers, handler)
	}
	b.mu.RUnlock()

	for _, handler := range handlers {
		handler(append([]byte(nil), msg...))
	}
	return nil
}

func (b *memoryBroker) NumSubscribers(ctx context.Context, channel string) (int64, error) {
	if err := normalizeContext(ctx).Err(); err != nil {
		return 0, err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return 0, errBrokerClosed
	}
	return int64(len(b.handlers[channel])), nil
}

func (b *memoryBroker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.handlers = make(map[string]map[uint64]func([]byte))
	return nil
}

type blockingBroker struct {
	mu          sync.RWMutex
	subscribers map[string]int64
}

func newBlockingBroker() *blockingBroker {
	return &blockingBroker{subscribers: make(map[string]int64)}
}

func (b *blockingBroker) addSubscriber(channel string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[channel]++
}

func (b *blockingBroker) Subscribe(context.Context, string, func([]byte)) error {
	return nil
}

func (b *blockingBroker) Publish(ctx context.Context, _ string, _ []byte) error {
	return normalizeContext(ctx).Err()
}

func (b *blockingBroker) NumSubscribers(ctx context.Context, channel string) (int64, error) {
	if err := normalizeContext(ctx).Err(); err != nil {
		return 0, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.subscribers[channel], nil
}

func (b *blockingBroker) Close() error {
	return nil
}
