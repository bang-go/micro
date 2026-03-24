package udpx_test

import (
	"bytes"
	"errors"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type testAddr string

func (a testAddr) Network() string { return "udp" }
func (a testAddr) String() string  { return string(a) }

type fakeDatagram struct {
	mu sync.Mutex

	readPackets [][]byte
	readAddrs   []net.Addr
	writes      [][]byte
	writeAddrs  []net.Addr

	readDeadline  time.Time
	writeDeadline time.Time

	localAddr  net.Addr
	remoteAddr net.Addr

	closeCount int
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newFakeDatagram() *fakeDatagram {
	return &fakeDatagram{
		localAddr:  testAddr("127.0.0.1:10000"),
		remoteAddr: testAddr("127.0.0.1:20000"),
	}
}

func (f *fakeDatagram) enqueueRead(data []byte, addr net.Addr) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readPackets = append(f.readPackets, append([]byte(nil), data...))
	f.readAddrs = append(f.readAddrs, addr)
}

func (f *fakeDatagram) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.readPackets) == 0 {
		return 0, errors.New("no packets queued")
	}
	packet := f.readPackets[0]
	f.readPackets = f.readPackets[1:]
	if len(f.readAddrs) > 0 {
		f.readAddrs = f.readAddrs[1:]
	}
	return copy(p, packet), nil
}

func (f *fakeDatagram) ReadFrom(p []byte) (int, net.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.readPackets) == 0 {
		return 0, nil, errors.New("no packets queued")
	}
	packet := f.readPackets[0]
	addr := f.remoteAddr
	if len(f.readAddrs) > 0 {
		addr = f.readAddrs[0]
		f.readAddrs = f.readAddrs[1:]
	}
	f.readPackets = f.readPackets[1:]
	return copy(p, packet), addr, nil
}

func (f *fakeDatagram) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, append([]byte(nil), p...))
	f.writeAddrs = append(f.writeAddrs, f.remoteAddr)
	return len(p), nil
}

func (f *fakeDatagram) WriteTo(p []byte, addr net.Addr) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, append([]byte(nil), p...))
	f.writeAddrs = append(f.writeAddrs, addr)
	return len(p), nil
}

func (f *fakeDatagram) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCount++
	return nil
}

func (f *fakeDatagram) LocalAddr() net.Addr {
	return f.localAddr
}

func (f *fakeDatagram) RemoteAddr() net.Addr {
	return f.remoteAddr
}

func (f *fakeDatagram) SetDeadline(time.Time) error {
	return nil
}

func (f *fakeDatagram) SetReadDeadline(deadline time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readDeadline = deadline
	return nil
}

func (f *fakeDatagram) SetWriteDeadline(deadline time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeDeadline = deadline
	return nil
}

type stubConn struct {
	net.Conn
}

type readPacket struct {
	data      []byte
	addr      *net.UDPAddr
	truncated bool
	err       error
}

type writePacket struct {
	data []byte
	addr net.Addr
}

type fakePacketConn struct {
	readCh  chan readPacket
	writeCh chan writePacket
	closed  chan struct{}

	localAddr net.Addr

	closeOnce sync.Once
	closeErr  error
}

func newFakePacketConn() *fakePacketConn {
	return &fakePacketConn{
		readCh:    make(chan readPacket, 16),
		writeCh:   make(chan writePacket, 16),
		closed:    make(chan struct{}),
		localAddr: testAddr("127.0.0.1:9999"),
	}
}

func (f *fakePacketConn) enqueuePacket(data []byte, addr *net.UDPAddr) {
	f.readCh <- readPacket{data: append([]byte(nil), data...), addr: addr}
}

func (f *fakePacketConn) enqueueTruncatedPacket(data []byte, addr *net.UDPAddr) {
	f.readCh <- readPacket{data: append([]byte(nil), data...), addr: addr, truncated: true}
}

func (f *fakePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	select {
	case <-f.closed:
		return 0, nil, net.ErrClosed
	case packet := <-f.readCh:
		if packet.err != nil {
			return 0, nil, packet.err
		}
		return copy(p, packet.data), packet.addr, nil
	}
}

func (f *fakePacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	select {
	case <-f.closed:
		return 0, net.ErrClosed
	case f.writeCh <- writePacket{data: append([]byte(nil), p...), addr: addr}:
		return len(p), nil
	}
}

func (f *fakePacketConn) Close() error {
	f.closeOnce.Do(func() {
		close(f.closed)
	})
	return f.closeErr
}

func (f *fakePacketConn) LocalAddr() net.Addr {
	return f.localAddr
}

func (f *fakePacketConn) SetDeadline(time.Time) error {
	return nil
}

func (f *fakePacketConn) SetReadDeadline(time.Time) error {
	return nil
}

func (f *fakePacketConn) SetWriteDeadline(time.Time) error {
	return nil
}

func (f *fakePacketConn) ReadMsgUDP(p, _ []byte) (int, int, int, *net.UDPAddr, error) {
	select {
	case <-f.closed:
		return 0, 0, 0, nil, net.ErrClosed
	case packet := <-f.readCh:
		if packet.err != nil {
			return 0, 0, 0, nil, packet.err
		}
		n := copy(p, packet.data)
		flags := 0
		if packet.truncated {
			flags = syscall.MSG_TRUNC
		}
		return n, 0, flags, packet.addr, nil
	}
}

func (f *fakePacketConn) nextWrite(timeout time.Duration) (writePacket, error) {
	select {
	case packet := <-f.writeCh:
		return packet, nil
	case <-time.After(timeout):
		return writePacket{}, errors.New("timed out waiting for write")
	}
}

func assertCollectorRegistered(t interface {
	Helper()
	Fatal(...any)
	Fatalf(string, ...any)
}, reg *prometheus.Registry, collector prometheus.Collector) {
	t.Helper()

	err := reg.Register(collector)
	if err == nil {
		t.Fatal("expected collector to already be registered")
	}
	if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
		t.Fatalf("Register() error = %v, want AlreadyRegisteredError", err)
	}
}

func assertCollectorNotRegistered(t interface {
	Helper()
	Fatalf(string, ...any)
}, reg *prometheus.Registry, collector prometheus.Collector) {
	t.Helper()

	if err := reg.Register(collector); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
}
