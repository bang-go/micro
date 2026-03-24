package tcpx

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bang-go/opt"
)

type Connect interface {
	Read([]byte) (int, error)
	ReadFull([]byte) error
	Write([]byte) (int, error)
	WriteFull([]byte) error
	SetReadTimeout(time.Duration)
	SetWriteTimeout(time.Duration)
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	Conn() net.Conn
	Close() error
}

type connectEntity struct {
	conn net.Conn

	readTimeout  atomic.Int64
	writeTimeout atomic.Int64

	readMu  sync.Mutex
	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error

	onRead  func(int)
	onWrite func(int)
}

func NewConnect(conn net.Conn, opts ...opt.Option[connectOptions]) (Connect, error) {
	if conn == nil {
		return nil, ErrNilConn
	}

	options := &connectOptions{}
	opt.Each(options, opts...)

	c := &connectEntity{
		conn:    conn,
		onRead:  options.onRead,
		onWrite: options.onWrite,
	}
	c.readTimeout.Store(int64(options.readTimeout))
	c.writeTimeout.Store(int64(options.writeTimeout))
	return c, nil
}

func (c *connectEntity) Read(data []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if err := c.conn.SetReadDeadline(c.deadline(c.readTimeout.Load())); err != nil {
		return 0, err
	}

	n, err := c.conn.Read(data)
	c.observeRead(n)
	return n, err
}

func (c *connectEntity) ReadFull(data []byte) error {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if err := c.conn.SetReadDeadline(c.deadline(c.readTimeout.Load())); err != nil {
		return err
	}

	remaining := data
	for len(remaining) > 0 {
		n, err := c.conn.Read(remaining)
		c.observeRead(n)
		remaining = remaining[n:]
		if n == 0 && err == nil {
			return io.ErrNoProgress
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *connectEntity) Write(data []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.conn.SetWriteDeadline(c.deadline(c.writeTimeout.Load())); err != nil {
		return 0, err
	}

	n, err := c.conn.Write(data)
	c.observeWrite(n)
	return n, err
}

func (c *connectEntity) WriteFull(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.conn.SetWriteDeadline(c.deadline(c.writeTimeout.Load())); err != nil {
		return err
	}

	remaining := data
	for len(remaining) > 0 {
		n, err := c.conn.Write(remaining)
		c.observeWrite(n)
		remaining = remaining[n:]
		if n == 0 && err == nil {
			return io.ErrNoProgress
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *connectEntity) SetReadTimeout(timeout time.Duration) {
	c.readTimeout.Store(int64(timeout))
}

func (c *connectEntity) SetWriteTimeout(timeout time.Duration) {
	c.writeTimeout.Store(int64(timeout))
}

func (c *connectEntity) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *connectEntity) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *connectEntity) Conn() net.Conn {
	return c.conn
}

func (c *connectEntity) Close() error {
	c.closeOnce.Do(func() {
		err := c.conn.Close()
		if err != nil && !isClosedConnectionError(err) {
			c.closeErr = err
		}
	})
	return c.closeErr
}

func (c *connectEntity) deadline(raw int64) time.Time {
	timeout := time.Duration(raw)
	if timeout <= 0 {
		return time.Time{}
	}
	return time.Now().Add(timeout)
}

func (c *connectEntity) observeRead(n int) {
	if n > 0 && c.onRead != nil {
		c.onRead(n)
	}
}

func (c *connectEntity) observeWrite(n int) {
	if n > 0 && c.onWrite != nil {
		c.onWrite(n)
	}
}
