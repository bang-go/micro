package wsx

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/bang-go/opt"
	"github.com/coder/websocket"
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

	// Conn returns underlying connection (use with caution).
	Conn() *websocket.Conn

	// ID returns the unique identifier for this connection
	ID() string
	// SetID sets the unique identifier
	SetID(string)

	// Get retrieves a value from metadata
	Get(key string) (value interface{}, exists bool)
	// Set stores a value in metadata
	Set(key string, value interface{})
}

type connectEntity struct {
	conn *websocket.Conn

	id     string
	meta   map[string]interface{}
	metaMu sync.RWMutex

	heartbeatInterval time.Duration
	readTimeout       time.Duration
	writeTimeout      time.Duration

	// Outbound channel
	sendChan chan message

	closed chan struct{}
	once   sync.Once
}

func NewConnect(conn *websocket.Conn, opts ...opt.Option[connectOptions]) Connect {
	options := &connectOptions{
		heartbeatInterval: 30 * time.Second,
		readTimeout:       60 * time.Second,
		writeTimeout:      10 * time.Second,
	}
	opt.Each(options, opts...)

	if options.sendBufferSize == 0 {
		options.sendBufferSize = 256
	}

	c := &connectEntity{
		conn:              conn,
		heartbeatInterval: options.heartbeatInterval,
		readTimeout:       options.readTimeout,
		writeTimeout:      options.writeTimeout,
		sendChan:          make(chan message, options.sendBufferSize),
		closed:            make(chan struct{}),
		meta:              make(map[string]interface{}),
	}

	// Metrics: Increment active connections
	connActive.Inc()

	// Start write loop
	go c.writeLoop()

	return c
}

func (c *connectEntity) writeLoop() {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	defer func() {
		// Metrics: Decrement active connections
		connActive.Dec()
	}()

	for {
		select {
		case <-c.closed:
			return

		case msg := <-c.sendChan:
			// Write message
			ctx, cancel := context.WithTimeout(context.Background(), c.writeTimeout)
			err := c.conn.Write(ctx, msg.typ, msg.data)
			cancel()
			if err != nil {
				// Log? Close?
				msgSent.WithLabelValues("error").Inc()
				c.Close()
				return
			}
			msgSent.WithLabelValues("success").Inc()

		case <-ticker.C:
			// Send Ping
			if c.heartbeatInterval > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), c.writeTimeout)
				err := c.conn.Ping(ctx)
				cancel()
				if err != nil {
					c.Close()
					return
				}
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

func (c *connectEntity) send(ctx context.Context, msg message) error {
	select {
	case <-c.closed:
		return fmt.Errorf("connection closed")
	case c.sendChan <- msg:
		return nil
	case <-ctx.Done():
		msgSent.WithLabelValues("dropped").Inc()
		return ctx.Err()
	}
}

func (c *connectEntity) ReadMessage(ctx context.Context) (websocket.MessageType, []byte, error) {
	// Read Loop should run in its own goroutine usually if we want full duplex?
	// But standard usage is user calls ReadMessage in a loop.

	var cancel context.CancelFunc
	if c.readTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, c.readTimeout)
		defer cancel()
	}

	mt, data, err := c.conn.Read(ctx)
	if err != nil {
		return 0, nil, err
	}
	msgReceived.Inc()
	return mt, data, nil
}

func (c *connectEntity) Close() error {
	c.once.Do(func() {
		close(c.closed)
		// Close sendChan? No, might panic writers. GC will handle it.
		_ = c.conn.Close(websocket.StatusNormalClosure, "closed")
	})
	return nil
}

func (c *connectEntity) Conn() *websocket.Conn {
	return c.conn
}

func (c *connectEntity) ID() string {
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	return c.id
}

func (c *connectEntity) SetID(id string) {
	c.metaMu.Lock()
	defer c.metaMu.Unlock()
	c.id = id
}

func (c *connectEntity) Get(key string) (value interface{}, exists bool) {
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	value, exists = c.meta[key]
	return
}

func (c *connectEntity) Set(key string, value interface{}) {
	c.metaMu.Lock()
	defer c.metaMu.Unlock()
	c.meta[key] = value
}
