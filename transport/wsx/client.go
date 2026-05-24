package wsx

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/bang-go/opt"
	"github.com/coder/websocket"
)

type Client interface {
	Start(context.Context) error
	Close() error
	OnConnect(func(context.Context, Connect))
	OnMessage(func(context.Context, websocket.MessageType, []byte))
	OnDisconnect(func(context.Context, error))
}

type clientEntity struct {
	addr    string
	options *clientOptions

	hookMu       sync.RWMutex
	onConnect    func(context.Context, Connect)
	onMessage    func(context.Context, websocket.MessageType, []byte)
	onDisconnect func(context.Context, error)

	connMu sync.RWMutex
	conn   Connect

	stateMu          sync.Mutex
	started          bool
	firstConnect     chan error
	firstConnectOnce sync.Once
	runCtx           context.Context
	runCancel        context.CancelFunc

	closed     chan struct{}
	closeOnce  sync.Once
	closeErrMu sync.Mutex
	closeErr   error
}

func NewClient(addr string, opts ...opt.Option[clientOptions]) Client {
	options := &clientOptions{
		dialTimeout:          5 * time.Second,
		reconnectInterval:    2 * time.Second,
		maxReconnectAttempts: -1, // infinite
	}
	opt.Each(options, opts...)
	if options.reconnectInterval <= 0 {
		options.reconnectInterval = 2 * time.Second
	}

	return &clientEntity{
		addr:         addr,
		options:      options,
		closed:       make(chan struct{}),
		firstConnect: make(chan error, 1),
	}
}

func (c *clientEntity) Start(ctx context.Context) error {
	ctx = normalizeContext(ctx)

	c.stateMu.Lock()
	if c.isClosed() {
		c.stateMu.Unlock()
		return errClientClosed
	}
	if c.started {
		c.stateMu.Unlock()
		return errClientAlreadyStarted
	}
	c.started = true
	c.runCtx, c.runCancel = context.WithCancel(context.Background())
	runCtx := c.runCtx
	c.stateMu.Unlock()

	go c.loop(runCtx)

	select {
	case err := <-c.firstConnect:
		return err
	case <-ctx.Done():
		return errors.Join(ctx.Err(), c.Close())
	case <-c.closed:
		return errClientClosed
	}
}

func (c *clientEntity) loop(ctx context.Context) {
	reconnectAttempts := 0

	for {
		if c.shouldStop() {
			c.signalFirstConnect(errClientClosed)
			return
		}

		// Dial
		dialCtx := ctx
		var cancel context.CancelFunc = func() {}
		if c.options.dialTimeout > 0 {
			dialCtx, cancel = context.WithTimeout(ctx, c.options.dialTimeout)
		}
		dialOpts := &websocket.DialOptions{
			HTTPHeader: c.options.httpHeader,
		}
		conn, _, err := websocket.Dial(dialCtx, c.addr, dialOpts)
		cancel()

		if err != nil {
			c.handleDisconnect(ctx, err)
			reconnectAttempts++
			if c.options.maxReconnectAttempts >= 0 && reconnectAttempts > c.options.maxReconnectAttempts {
				c.signalFirstConnect(fmt.Errorf("wsx: initial connect failed after %d attempts: %w", reconnectAttempts, err))
				return
			}
			if !c.waitReconnect(ctx, c.calculateBackoff(reconnectAttempts)) {
				c.signalFirstConnect(errClientClosed)
				return
			}
			continue
		}

		// Reset attempts on successful connection
		reconnectAttempts = 0

		// Wrap connection
		wsConn := NewConnect(conn, c.addr, c.options.connectOpts...)
		c.setConn(wsConn)

		c.signalFirstConnect(nil)
		if err := c.handleConnect(ctx, wsConn); err != nil {
			c.clearConn(wsConn)
			closeErr := wsConn.Close()
			c.handleDisconnect(ctx, err)
			c.appendCloseErr(closeErr, c.Close())
			return
		}

		// Block reading
		for {
			mt, msg, err := wsConn.ReadMessage(ctx)
			if err != nil {
				c.clearConn(wsConn)
				closeErr := wsConn.Close()
				if c.shouldStop() || errors.Is(err, context.Canceled) {
					return
				}
				c.handleDisconnect(ctx, errors.Join(err, closeErr))
				break
			}
			if err := c.handleMessage(ctx, mt, msg); err != nil {
				c.clearConn(wsConn)
				closeErr := wsConn.Close()
				c.handleDisconnect(ctx, err)
				c.appendCloseErr(closeErr, c.Close())
				return
			}
		}

		if !c.waitReconnect(ctx, c.calculateBackoff(0)) {
			return
		}
	}
}

func (c *clientEntity) calculateBackoff(attempt int) time.Duration {
	if attempt == 0 {
		return c.options.reconnectInterval
	}

	backoff := c.options.reconnectInterval
	for i := 0; i < attempt; i++ {
		if backoff >= 30*time.Second/2 {
			backoff = 30 * time.Second
			break
		}
		backoff *= 2
	}
	if backoff > 30*time.Second {
		backoff = 30 * time.Second
	}

	// Add jitter: ±20%
	jitter := time.Duration((rand.Float64()*0.4 - 0.2) * float64(backoff))
	delay := backoff + jitter
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func (c *clientEntity) Close() error {
	c.closeOnce.Do(func() {
		c.signalFirstConnect(errClientClosed)
		close(c.closed)

		c.stateMu.Lock()
		cancel := c.runCancel
		c.stateMu.Unlock()
		if cancel != nil {
			cancel()
		}

		if conn := c.currentConn(); conn != nil {
			c.appendCloseErr(conn.Close())
		}
	})
	return c.currentCloseErr()
}

func (c *clientEntity) appendCloseErr(errs ...error) {
	c.closeErrMu.Lock()
	defer c.closeErrMu.Unlock()
	c.closeErr = errors.Join(append([]error{c.closeErr}, errs...)...)
}

func (c *clientEntity) currentCloseErr() error {
	c.closeErrMu.Lock()
	defer c.closeErrMu.Unlock()
	return c.closeErr
}

func (c *clientEntity) OnConnect(f func(context.Context, Connect)) {
	c.hookMu.Lock()
	defer c.hookMu.Unlock()
	c.onConnect = f
}

func (c *clientEntity) OnMessage(f func(context.Context, websocket.MessageType, []byte)) {
	c.hookMu.Lock()
	defer c.hookMu.Unlock()
	c.onMessage = f
}

func (c *clientEntity) OnDisconnect(f func(context.Context, error)) {
	c.hookMu.Lock()
	defer c.hookMu.Unlock()
	c.onDisconnect = f
}

func (c *clientEntity) waitReconnect(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return !c.shouldStop()
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return !c.shouldStop()
	case <-ctx.Done():
		return false
	case <-c.closed:
		return false
	}
}

func (c *clientEntity) signalFirstConnect(err error) {
	c.firstConnectOnce.Do(func() {
		c.firstConnect <- err
	})
}

func (c *clientEntity) setConn(conn Connect) {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	c.conn = conn
}

func (c *clientEntity) clearConn(conn Connect) {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn == conn {
		c.conn = nil
	}
}

func (c *clientEntity) currentConn() Connect {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.conn
}

func (c *clientEntity) handleConnect(ctx context.Context, conn Connect) error {
	c.hookMu.RLock()
	hook := c.onConnect
	c.hookMu.RUnlock()
	if hook != nil {
		return invokeClientHookSafely("on_connect", func() {
			hook(ctx, conn)
		})
	}
	return nil
}

func (c *clientEntity) handleMessage(ctx context.Context, typ websocket.MessageType, msg []byte) error {
	c.hookMu.RLock()
	hook := c.onMessage
	c.hookMu.RUnlock()
	if hook != nil {
		return invokeClientHookSafely("on_message", func() {
			hook(ctx, typ, msg)
		})
	}
	return nil
}

func (c *clientEntity) handleDisconnect(ctx context.Context, err error) {
	if err == nil || c.shouldStop() {
		return
	}

	c.hookMu.RLock()
	hook := c.onDisconnect
	c.hookMu.RUnlock()
	if hook != nil {
		if hookErr := invokeClientHookSafely("on_disconnect", func() {
			hook(ctx, err)
		}); hookErr != nil {
			c.appendCloseErr(hookErr)
		}
	}
}

func invokeClientHookSafely(name string, fn func()) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("wsx: client %s callback panic: %v", name, recovered)
		}
	}()
	fn()
	return nil
}

func (c *clientEntity) shouldStop() bool {
	if c.isClosed() {
		return true
	}

	c.stateMu.Lock()
	runCtx := c.runCtx
	c.stateMu.Unlock()
	return runCtx != nil && runCtx.Err() != nil
}

func (c *clientEntity) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}
