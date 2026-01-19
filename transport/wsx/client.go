package wsx

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/bang-go/opt"
	"github.com/coder/websocket"
)

type Client interface {
	Connect(context.Context) error
	Close()
	OnConnect(func(context.Context, Connect))
	OnMessage(func(context.Context, websocket.MessageType, []byte))
	OnClose(func(context.Context, error))
}

type clientEntity struct {
	addr    string
	options *clientOptions

	onConnect func(context.Context, Connect)
	onMessage func(context.Context, websocket.MessageType, []byte)
	onClose   func(context.Context, error)

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
				return
			}

			c.handleError(ctx, err)
			reconnectAttempts++
			if c.options.maxReconnectAttempts >= 0 && reconnectAttempts > c.options.maxReconnectAttempts {
				return
			}

			// Exponential backoff with jitter
			delay := c.calculateBackoff(reconnectAttempts)
			select {
			case <-time.After(delay):
				continue
			case <-ctx.Done():
				return
			case <-c.closed:
				return
			}
		}

		if firstTry {
			firstTry = false
			c.firstConnect <- nil
		}

		// Reset attempts on successful connection
		reconnectAttempts = 0

		// Wrap connection
		wsConn := NewConnect(conn, c.options.connectOpts...)
		c.mu.Lock()
		c.conn = wsConn
		c.mu.Unlock()

		if c.onConnect != nil {
			c.onConnect(ctx, wsConn)
		}

		// Block reading
		for {
			mt, msg, err := wsConn.ReadMessage(ctx)
			if err != nil {
				c.handleError(ctx, err)
				wsConn.Close()
				break
			}
			if c.onMessage != nil {
				c.onMessage(ctx, mt, msg)
			}
		}

		// If connection drops, wait before next attempt
		delay := c.calculateBackoff(0) // use base interval
		time.Sleep(delay)
	}
}

func (c *clientEntity) calculateBackoff(attempt int) time.Duration {
	if attempt == 0 {
		return c.options.reconnectInterval
	}

	// Exponential backoff: base * 2^attempt
	backoff := float64(c.options.reconnectInterval) * float64(int(1)<<uint(attempt))

	// Max backoff: 30s
	if backoff > float64(30*time.Second) {
		backoff = float64(30 * time.Second)
	}

	// Add jitter: ±20%
	jitter := (rand.Float64()*0.4 - 0.2) * backoff
	return time.Duration(backoff + jitter)
}

func (c *clientEntity) handleError(ctx context.Context, err error) {
	if c.onClose != nil {
		c.onClose(ctx, err)
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

func (c *clientEntity) OnConnect(f func(context.Context, Connect)) {
	c.onConnect = f
}

func (c *clientEntity) OnMessage(f func(context.Context, websocket.MessageType, []byte)) {
	c.onMessage = f
}

func (c *clientEntity) OnClose(f func(context.Context, error)) {
	c.onClose = f
}
