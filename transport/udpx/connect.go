package udpx

import (
	"net"
	"time"

	"github.com/bang-go/opt"
)

type Connect interface {
	Send([]byte) error
	SendToUdp([]byte, *net.UDPAddr) error
	Receive([]byte) error
	ReceiveFromUdp([]byte) (*net.UDPAddr, error)
	Conn() *net.UDPConn
	SetTimeout(time.Duration)
	Close()
}

type connectEntity struct {
	conn    *net.UDPConn
	timeout time.Duration
}

func NewConnect(conn *net.UDPConn, opts ...opt.Option[connectOptions]) Connect {
	options := &connectOptions{}
	opt.Each(options, opts...)
	return &connectEntity{conn: conn, timeout: options.timeout}
}

func (c *connectEntity) Send(data []byte) (err error) {
	if c.timeout > 0 {
		if err = c.conn.SetWriteDeadline(c.calcDeadline()); err != nil {
			return
		}
	}
	_, err = c.conn.Write(data)
	return
}

// SendToUdp 发送数据
func (c *connectEntity) SendToUdp(data []byte, udpAddr *net.UDPAddr) (err error) {
	if c.timeout > 0 {
		if err = c.conn.SetWriteDeadline(c.calcDeadline()); err != nil {
			return
		}
	}
	_, err = c.conn.WriteToUDP(data, udpAddr)
	return
}

func (c *connectEntity) Receive(data []byte) (err error) {
	if c.timeout > 0 {
		if err = c.conn.SetReadDeadline(c.calcDeadline()); err != nil {
			return
		}
	}
	_, err = c.conn.Read(data)
	return
}

// ReceiveFromUdp 接受数据
func (c *connectEntity) ReceiveFromUdp(data []byte) (udpAddr *net.UDPAddr, err error) {
	if c.timeout > 0 {
		if err = c.conn.SetReadDeadline(c.calcDeadline()); err != nil {
			return
		}
	}
	_, udpAddr, err = c.conn.ReadFromUDP(data)
	return
}
func (c *connectEntity) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

func (c *connectEntity) Conn() *net.UDPConn {
	return c.conn
}

func (c *connectEntity) Close() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
}
func (c *connectEntity) calcDeadline() time.Time {
	return time.Now().Add(c.timeout)
}
