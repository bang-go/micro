package wsx

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/bang-go/opt"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// Message struct for internal queue
type message struct {
	typ  websocket.MessageType
	data []byte
}

type Connect interface {
	// SendText queues a text message. It returns nil if queued, error if closed.
	// Context is used for queuing timeout if channel is full.
	SendText(context.Context, string) error

	// SendBinary queues a binary message.
	SendBinary(context.Context, []byte) error

	// SendJSON queues a JSON message.
	SendJSON(context.Context, interface{}) error

	// ReadMessage blocks until a message is received or context done.
	ReadMessage(context.Context) (websocket.MessageType, []byte, error)

	// Close closes the connection and loops.
	Close() error

	// RemoteAddr returns the remote network address
	RemoteAddr() string

	// UserID returns the immutable business identity associated with this connection.
	UserID() string

	// SessionID returns the unique physical session identifier
	SessionID() string
}

type roomMember interface {
	Connect
	roomCount() int
	rooms() []string
	joinRoom(room string)
	leaveRoom(room string)
}

type connectEntity struct {
	conn *websocket.Conn

	userID     string
	sessionID  string
	remoteAddr string
	roomSet    map[string]struct{}
	roomMu     sync.RWMutex

	heartbeatInterval time.Duration
	readTimeout       time.Duration
	writeTimeout      time.Duration

	// Outbound channel
	sendChan chan message

	closed      chan struct{}
	closeCtx    context.Context
	closeCancel context.CancelFunc
	once        sync.Once

	skipObservability bool
}

func NewConnect(conn *websocket.Conn, remoteAddr string, opts ...opt.Option[connectOptions]) Connect {
	options := &connectOptions{
		heartbeatInterval: 20 * time.Second,
		readTimeout:       0, // Default to 0 (no timeout) to avoid killing idle connections with active heartbeats
		writeTimeout:      10 * time.Second,
	}
	opt.Each(options, opts...)

	if options.sendBufferSize <= 0 {
		options.sendBufferSize = 256
	}

	registerWSMetrics()

	c := &connectEntity{
		conn:              conn,
		userID:            options.userID,
		sessionID:         uuid.NewString(),
		remoteAddr:        remoteAddr,
		heartbeatInterval: options.heartbeatInterval,
		readTimeout:       options.readTimeout,
		writeTimeout:      options.writeTimeout,
		sendChan:          make(chan message, options.sendBufferSize),
		closed:            make(chan struct{}),
		roomSet:           make(map[string]struct{}),
		skipObservability: options.skipObservability,
	}
	c.closeCtx, c.closeCancel = context.WithCancel(context.Background())

	// Metrics: Increment active connections
	if !c.skipObservability {
		connActive.Inc()
	}

	// Start write loop
	go c.writeLoop()

	return c
}

func (c *connectEntity) RemoteAddr() string {
	return c.remoteAddr
}

func (c *connectEntity) SessionID() string {
	return c.sessionID
}

func (c *connectEntity) writeLoop() {
	// Panic Recovery
	defer func() {
		if r := recover(); r != nil {
			// Log panic?
			// For now just close connection
			c.Close()
		}
	}()

	var ticker *time.Ticker
	var tickerC <-chan time.Time
	if c.heartbeatInterval > 0 {
		ticker = time.NewTicker(c.heartbeatInterval)
		defer ticker.Stop()
		tickerC = ticker.C
	}

	defer func() {
		// Metrics: Decrement active connections
		if !c.skipObservability {
			connActive.Dec()
		}
	}()

	for {
		select {
		case <-c.closed:
			return

		case msg, ok := <-c.sendChan:
			if !ok {
				return
			}
			// Write message
			wCtx, wCancel := c.newWriteContext()
			err := c.conn.Write(wCtx, msg.typ, msg.data)
			wCancel()
			if err != nil {
				// Log? Close?
				if !c.skipObservability {
					msgSent.WithLabelValues("error").Inc()
				}
				c.Close()
				return
			}
			if !c.skipObservability {
				msgSent.WithLabelValues("success").Inc()
			}

		case <-tickerC:
			// Send Ping
			pCtx, pCancel := c.newWriteContext()
			err := c.conn.Ping(pCtx)
			pCancel()
			if err != nil {
				c.Close()
				return
			}
		}
	}
}

func (c *connectEntity) SendText(ctx context.Context, text string) error {
	return c.send(ctx, message{typ: websocket.MessageText, data: []byte(text)})
}

func (c *connectEntity) SendBinary(ctx context.Context, data []byte) error {
	return c.send(ctx, message{typ: websocket.MessageBinary, data: data})
}

func (c *connectEntity) SendJSON(ctx context.Context, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.send(ctx, message{typ: websocket.MessageText, data: data})
}

func (c *connectEntity) send(ctx context.Context, msg message) (err error) {
	ctx = normalizeContext(ctx)
	defer func() {
		if recover() != nil {
			err = errConnectionClosed
		}
	}()

	queued := msg
	if msg.data != nil {
		queued.data = append([]byte(nil), msg.data...)
	}

	select {
	case <-c.closeCtx.Done():
		return errConnectionClosed
	case c.sendChan <- queued:
		return nil
	case <-ctx.Done():
		if !c.skipObservability {
			msgSent.WithLabelValues("dropped").Inc()
		}
		return ctx.Err()
	}
}

func (c *connectEntity) ReadMessage(ctx context.Context) (websocket.MessageType, []byte, error) {
	baseCtx := normalizeContext(ctx)
	if c.readTimeout > 0 {
		var cancel context.CancelFunc
		baseCtx, cancel = context.WithTimeout(baseCtx, c.readTimeout)
		defer cancel()
	}

	readCtx, cancelRead := context.WithCancel(baseCtx)
	defer cancelRead()

	stop := context.AfterFunc(c.closeCtx, cancelRead)
	defer func() {
		_ = stop()
	}()

	// Read Loop should run in its own goroutine usually if we want full duplex?
	// But standard usage is user calls ReadMessage in a loop.

	// In coder/websocket, we don't use SetReadDeadline like in gorilla.
	// Instead, we pass the original context. The connection will be closed
	// if the context is cancelled or the Ping loop detects a failure.

	mt, data, err := c.conn.Read(readCtx)
	if err != nil {
		return 0, nil, err
	}
	if !c.skipObservability {
		msgReceived.Inc()
	}
	return mt, data, nil
}

func (c *connectEntity) Close() error {
	c.once.Do(func() {
		c.closeCancel()
		close(c.closed)
		close(c.sendChan)
		_ = c.conn.Close(websocket.StatusNormalClosure, "closed")
	})
	return nil
}

func (c *connectEntity) UserID() string {
	return c.userID
}

func (c *connectEntity) rooms() []string {
	c.roomMu.RLock()
	defer c.roomMu.RUnlock()
	rooms := make([]string, 0, len(c.roomSet))
	for r := range c.roomSet {
		rooms = append(rooms, r)
	}
	return rooms
}

func (c *connectEntity) roomCount() int {
	c.roomMu.RLock()
	defer c.roomMu.RUnlock()
	return len(c.roomSet)
}

func (c *connectEntity) joinRoom(room string) {
	c.roomMu.Lock()
	defer c.roomMu.Unlock()
	c.roomSet[room] = struct{}{}
}

func (c *connectEntity) leaveRoom(room string) {
	c.roomMu.Lock()
	defer c.roomMu.Unlock()
	delete(c.roomSet, room)
}

func (c *connectEntity) newWriteContext() (context.Context, context.CancelFunc) {
	if c.writeTimeout > 0 {
		return context.WithTimeout(context.Background(), c.writeTimeout)
	}
	return context.WithCancel(context.Background())
}
