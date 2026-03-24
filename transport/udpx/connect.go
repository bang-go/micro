package udpx

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bang-go/opt"
)

type datagramConn interface {
	net.Conn
	ReadFrom([]byte) (int, net.Addr, error)
	WriteTo([]byte, net.Addr) (int, error)
}

type Connect interface {
	Read([]byte) (int, error)
	ReadFrom([]byte) (int, net.Addr, error)
	Write([]byte) (int, error)
	WriteTo([]byte, net.Addr) (int, error)
	SetReadTimeout(time.Duration)
	SetWriteTimeout(time.Duration)
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	Conn() net.Conn
	Close() error
}

type connectEntity struct {
	conn datagramConn

	readTimeout  atomic.Int64
	writeTimeout atomic.Int64

	readMu  sync.Mutex
	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
}

func NewConnect(conn net.Conn, opts ...opt.Option[connectOptions]) (Connect, error) {
	if conn == nil {
		return nil, ErrNilConn
	}

	rawConn, ok := conn.(datagramConn)
	if !ok {
		return nil, ErrDatagramConnRequired
	}

	options := &connectOptions{}
	opt.Each(options, opts...)

	c := &connectEntity{conn: rawConn}
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
	return c.conn.Read(data)
}

func (c *connectEntity) ReadFrom(data []byte) (int, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if err := c.conn.SetReadDeadline(c.deadline(c.readTimeout.Load())); err != nil {
		return 0, nil, err
	}
	return c.conn.ReadFrom(data)
}

func (c *connectEntity) Write(data []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.conn.SetWriteDeadline(c.deadline(c.writeTimeout.Load())); err != nil {
		return 0, err
	}
	return c.conn.Write(data)
}

func (c *connectEntity) WriteTo(data []byte, addr net.Addr) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.conn.SetWriteDeadline(c.deadline(c.writeTimeout.Load())); err != nil {
		return 0, err
	}
	return c.conn.WriteTo(data, addr)
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
