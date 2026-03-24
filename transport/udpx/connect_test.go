package udpx_test

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/bang-go/micro/transport/udpx"
)

func TestNewConnectValidation(t *testing.T) {
	_, err := udpx.NewConnect(nil)
	if !errors.Is(err, udpx.ErrNilConn) {
		t.Fatalf("NewConnect(nil) error = %v, want %v", err, udpx.ErrNilConn)
	}

	rawConn := &stubConn{}
	_, err = udpx.NewConnect(rawConn)
	if !errors.Is(err, udpx.ErrDatagramConnRequired) {
		t.Fatalf("NewConnect(non-datagram) error = %v, want %v", err, udpx.ErrDatagramConnRequired)
	}
}

func TestConnectReadWriteAndTimeouts(t *testing.T) {
	raw := newFakeDatagram()
	raw.remoteAddr = testAddr("127.0.0.1:20001")
	raw.enqueueRead([]byte("hello"), raw.remoteAddr)
	raw.enqueueRead([]byte("world"), testAddr("127.0.0.1:20002"))

	conn, err := udpx.NewConnect(raw,
		udpx.WithReadTimeout(2*time.Second),
		udpx.WithWriteTimeout(3*time.Second),
	)
	if err != nil {
		t.Fatalf("NewConnect: %v", err)
	}

	n, err := conn.Write([]byte("ping"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got, want := n, 4; got != want {
		t.Fatalf("write n = %d, want %d", got, want)
	}
	if raw.writeDeadline.IsZero() {
		t.Fatal("write deadline was not applied")
	}

	n, err = conn.WriteTo([]byte("pong"), testAddr("127.0.0.1:20003"))
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got, want := n, 4; got != want {
		t.Fatalf("writeTo n = %d, want %d", got, want)
	}

	buf := make([]byte, 16)
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got, want := string(buf[:n]), "hello"; got != want {
		t.Fatalf("read = %q, want %q", got, want)
	}
	if raw.readDeadline.IsZero() {
		t.Fatal("read deadline was not applied")
	}

	n, addr, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if got, want := string(buf[:n]), "world"; got != want {
		t.Fatalf("readFrom data = %q, want %q", got, want)
	}
	if got, want := addr.String(), "127.0.0.1:20002"; got != want {
		t.Fatalf("readFrom addr = %q, want %q", got, want)
	}

	conn.SetReadTimeout(0)
	conn.SetWriteTimeout(0)
	if _, err := conn.Write([]byte("!")); err != nil {
		t.Fatalf("Write after timeout reset: %v", err)
	}
	if !raw.writeDeadline.IsZero() {
		t.Fatal("write deadline was not cleared")
	}

	if got, want := conn.LocalAddr().String(), raw.localAddr.String(); got != want {
		t.Fatalf("LocalAddr = %q, want %q", got, want)
	}
	if got, want := conn.RemoteAddr().String(), raw.remoteAddr.String(); got != want {
		t.Fatalf("RemoteAddr = %q, want %q", got, want)
	}
	if _, ok := conn.Conn().(net.Conn); !ok {
		t.Fatal("Conn did not return a net.Conn")
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close idempotent: %v", err)
	}
	if got, want := raw.closeCount, 1; got != want {
		t.Fatalf("close count = %d, want %d", got, want)
	}
}
