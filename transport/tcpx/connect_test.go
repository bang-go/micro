package tcpx_test

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/bang-go/micro/transport/tcpx"
)

func TestNewConnectValidation(t *testing.T) {
	_, err := tcpx.NewConnect(nil)
	if !errors.Is(err, tcpx.ErrNilConn) {
		t.Fatalf("NewConnect(nil) error = %v, want %v", err, tcpx.ErrNilConn)
	}
}

func TestConnectReadWriteFullAndTimeouts(t *testing.T) {
	raw := &stubConn{
		readChunks:      [][]byte{[]byte("he"), []byte("llo")},
		writeChunkSize:  2,
		localAddrValue:  stubAddr("local"),
		remoteAddrValue: stubAddr("remote"),
	}

	conn, err := tcpx.NewConnect(raw,
		tcpx.WithReadTimeout(2*time.Second),
		tcpx.WithWriteTimeout(3*time.Second),
	)
	if err != nil {
		t.Fatalf("NewConnect: %v", err)
	}

	if err := conn.WriteFull([]byte("world")); err != nil {
		t.Fatalf("WriteFull: %v", err)
	}
	if got, want := raw.writes.String(), "world"; got != want {
		t.Fatalf("written = %q, want %q", got, want)
	}
	if raw.writeDeadline.IsZero() {
		t.Fatal("write deadline was not applied")
	}

	buf := make([]byte, 5)
	if err := conn.ReadFull(buf); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if got, want := string(buf), "hello"; got != want {
		t.Fatalf("read = %q, want %q", got, want)
	}
	if raw.readDeadline.IsZero() {
		t.Fatal("read deadline was not applied")
	}

	conn.SetReadTimeout(0)
	conn.SetWriteTimeout(0)
	if _, err := conn.Write([]byte("!")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !raw.writeDeadline.IsZero() {
		t.Fatal("write deadline was not cleared")
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close idempotent: %v", err)
	}
	if got, want := raw.closeCalls, 1; got != want {
		t.Fatalf("close calls = %d, want %d", got, want)
	}
}

type stubConn struct {
	readChunks [][]byte

	writes         bytes.Buffer
	writeChunkSize int

	readDeadline  time.Time
	writeDeadline time.Time

	closeCalls int

	localAddrValue  net.Addr
	remoteAddrValue net.Addr
}

func (c *stubConn) Read(p []byte) (int, error) {
	if len(c.readChunks) == 0 {
		return 0, io.EOF
	}
	chunk := c.readChunks[0]
	c.readChunks = c.readChunks[1:]
	n := copy(p, chunk)
	return n, nil
}

func (c *stubConn) Write(p []byte) (int, error) {
	if c.writeChunkSize <= 0 || c.writeChunkSize > len(p) {
		c.writeChunkSize = len(p)
	}
	n, err := c.writes.Write(p[:c.writeChunkSize])
	return n, err
}

func (c *stubConn) Close() error {
	c.closeCalls++
	return nil
}

func (c *stubConn) LocalAddr() net.Addr {
	return c.localAddrValue
}

func (c *stubConn) RemoteAddr() net.Addr {
	return c.remoteAddrValue
}

func (c *stubConn) SetDeadline(time.Time) error {
	return nil
}

func (c *stubConn) SetReadDeadline(deadline time.Time) error {
	c.readDeadline = deadline
	return nil
}

func (c *stubConn) SetWriteDeadline(deadline time.Time) error {
	c.writeDeadline = deadline
	return nil
}

type stubAddr string

func (a stubAddr) Network() string { return string(a) }
func (a stubAddr) String() string  { return string(a) }
