package ws

import (
	"context"
	"sync"
	"time"

	"github.com/bang-go/opt"
	"github.com/coder/websocket"
)

type Client interface {
	Connect(context.Context) error
	Close()
	OnConnect(func(Connect))
	OnMessage(func(websocket.MessageType, []byte))
	OnClose(func(error))
}

type clientEntity struct {
	addr    string
	options *clientOptions

	onConnect func(Connect)
	onMessage func(websocket.MessageType, []byte)
	onClose   func(error)

	conn   Connect
	closed chan struct{}
	mu     sync.Mutex

	// Add support for blocking connect
	firstConnect chan error
}

func NewClient(addr string, opts ...opt.Option[clientOptions]) Client {
	options := &clientOptions{
		dialTimeout:          5 * time.Second,
		reconnectInterval:    2 * time.Second,
		maxReconnectAttempts: -1, // infinite
	}
	opt.Each(options, opts...)

	return &clientEntity{
		addr:         addr,
		options:      options,
		closed:       make(chan struct{}),
		firstConnect: make(chan error, 1),
	}
}

func (c *clientEntity) Connect(ctx context.Context) error {
	go c.loop(ctx)

	// Wait for first connection result or context done
	select {
	case err := <-c.firstConnect:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *clientEntity) loop(ctx context.Context) {
	reconnectAttempts := 0
	firstTry := true

	for {
		select {
		case <-c.closed:
			return
		case <-ctx.Done():
			return
		default:
		}

		// Dial
		dialCtx, cancel := context.WithTimeout(ctx, c.options.dialTimeout)
		dialOpts := &websocket.DialOptions{
			HTTPHeader: c.options.httpHeader,
		}
		conn, _, err := websocket.Dial(dialCtx, c.addr, dialOpts)
		cancel()

		if err != nil {
			if firstTry {
				c.firstConnect <- err
				return // Stop if first try fails (user expects error)
			}

			c.handleError(err)
			reconnectAttempts++
			if c.options.maxReconnectAttempts >= 0 && reconnectAttempts > c.options.maxReconnectAttempts {
				return
			}
			time.Sleep(c.options.reconnectInterval)
			continue
		}

		if firstTry {
			firstTry = false
			c.firstConnect <- nil
		}

		// Reset attempts
		reconnectAttempts = 0

		// Wrap connection
		wsConn := NewConnect(conn, c.options.connectOpts...)
		c.mu.Lock()
		c.conn = wsConn
		c.mu.Unlock()

		if c.onConnect != nil {
			c.onConnect(wsConn)
		}

		// Block reading
		for {
			// Use context for read?
			// We should probably allow the loop context to cancel reading.
			mt, msg, err := wsConn.ReadMessage(ctx)
			if err != nil {
				c.handleError(err)
				wsConn.Close()
				break
			}
			if c.onMessage != nil {
				c.onMessage(mt, msg)
			}
		}

		time.Sleep(c.options.reconnectInterval)
	}
}

func (c *clientEntity) handleError(err error) {
	if c.onClose != nil {
		c.onClose(err)
	} else {
		// Log error if needed
		// fmt.Printf("wsx client error: %v\n", err)
	}
}

func (c *clientEntity) Close() {
	close(c.closed)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
	}
}

func (c *clientEntity) OnConnect(f func(Connect)) {
	c.onConnect = f
}

func (c *clientEntity) OnMessage(f func(websocket.MessageType, []byte)) {
	c.onMessage = f
}

func (c *clientEntity) OnClose(f func(error)) {
	c.onClose = f
}
