package wsx

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

type Connect interface {
	WriteMessage(context.Context, websocket.MessageType, []byte) error
	ReadMessage(context.Context) (websocket.MessageType, []byte, error)
	CloseNow() error
	RemoteAddr() string
	UserID() string
	SessionID() string
}

type roomMember interface {
	Connect
	roomCount() int
	rooms() []string
	joinRoom(string)
	leaveRoom(string)
}

type connectEntity struct {
	conn       *websocket.Conn
	userID     string
	sessionID  string
	remoteAddr string

	heartbeatInterval time.Duration
	writeTimeout      time.Duration

	roomMu  sync.RWMutex
	roomSet map[string]struct{}

	mu         sync.Mutex
	terminated bool
	closed     bool
	closeErr   error
	cancel     context.CancelFunc

	skipObservability bool
}

func newConnect(
	parent context.Context,
	conn *websocket.Conn,
	userID string,
	remoteAddr string,
	heartbeatInterval time.Duration,
	writeTimeout time.Duration,
	skipObservability bool,
) (*connectEntity, context.Context) {
	ctx, cancel := context.WithCancel(parent)
	c := &connectEntity{
		conn:              conn,
		userID:            userID,
		sessionID:         uuid.NewString(),
		remoteAddr:        remoteAddr,
		heartbeatInterval: heartbeatInterval,
		writeTimeout:      writeTimeout,
		roomSet:           make(map[string]struct{}),
		cancel:            cancel,
		skipObservability: skipObservability,
	}
	if !skipObservability {
		connActive.Inc()
	}
	if heartbeatInterval > 0 {
		go c.heartbeat(ctx)
	}
	return c, ctx
}

func (c *connectEntity) WriteMessage(ctx context.Context, typ websocket.MessageType, data []byte) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if !c.isOpen() {
		return ErrConnectionClosed
	}
	writeCtx := ctx
	cancel := func() {}
	if c.writeTimeout > 0 {
		writeCtx, cancel = context.WithTimeout(ctx, c.writeTimeout)
	}
	defer cancel()
	if err := c.conn.Write(writeCtx, typ, data); err != nil {
		if !c.skipObservability {
			msgSent.WithLabelValues("error").Inc()
		}
		c.markTerminated()
		return err
	}
	if !c.skipObservability {
		msgSent.WithLabelValues("success").Inc()
	}
	return nil
}

func (c *connectEntity) ReadMessage(ctx context.Context) (websocket.MessageType, []byte, error) {
	if err := validateContext(ctx); err != nil {
		return 0, nil, err
	}
	if !c.isOpen() {
		return 0, nil, ErrConnectionClosed
	}
	typ, data, err := c.conn.Read(ctx)
	if err != nil {
		c.markTerminated()
		return 0, nil, err
	}
	if !c.skipObservability {
		msgReceived.Inc()
	}
	return typ, data, nil
}

func (c *connectEntity) CloseNow() error {
	c.mu.Lock()
	if c.closed {
		err := c.closeErr
		c.mu.Unlock()
		return err
	}
	c.closed = true
	c.terminateLocked()
	c.closeErr = c.conn.CloseNow()
	err := c.closeErr
	c.mu.Unlock()
	return err
}

func (c *connectEntity) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx := ctx
			cancel := func() {}
			if c.writeTimeout > 0 {
				pingCtx, cancel = context.WithTimeout(ctx, c.writeTimeout)
			}
			err := c.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				c.markTerminated()
				return
			}
		}
	}
}

func (c *connectEntity) markTerminated() {
	c.mu.Lock()
	c.terminateLocked()
	c.mu.Unlock()
}

func (c *connectEntity) terminateLocked() {
	if c.terminated {
		return
	}
	c.terminated = true
	c.cancel()
	if !c.skipObservability {
		connActive.Dec()
	}
}

func (c *connectEntity) isOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.terminated
}

func (c *connectEntity) RemoteAddr() string { return c.remoteAddr }
func (c *connectEntity) UserID() string     { return c.userID }
func (c *connectEntity) SessionID() string  { return c.sessionID }

func (c *connectEntity) rooms() []string {
	c.roomMu.RLock()
	defer c.roomMu.RUnlock()
	result := make([]string, 0, len(c.roomSet))
	for room := range c.roomSet {
		result = append(result, room)
	}
	return result
}

func (c *connectEntity) roomCount() int {
	c.roomMu.RLock()
	defer c.roomMu.RUnlock()
	return len(c.roomSet)
}

func (c *connectEntity) joinRoom(room string) {
	c.roomMu.Lock()
	c.roomSet[room] = struct{}{}
	c.roomMu.Unlock()
}

func (c *connectEntity) leaveRoom(room string) {
	c.roomMu.Lock()
	delete(c.roomSet, room)
	c.roomMu.Unlock()
}
