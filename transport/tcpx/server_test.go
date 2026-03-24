package tcpx_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/micro/transport/tcpx"
	"github.com/prometheus/client_golang/prometheus"
)

func TestServerStartShutdownAndRecovery(t *testing.T) {
	listener := newPipeListener()

	var logs bytes.Buffer
	server := tcpx.NewServer(&tcpx.ServerConfig{
		Listener:     listener,
		EnableLogger: true,
		Logger: logger.New(
			logger.WithFormat("text"),
			logger.WithAddSource(false),
			logger.WithOutput(&logs),
		),
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), tcpx.HandlerFunc(func(ctx context.Context, conn tcpx.Connect) error {
			buf := make([]byte, 4)
			if err := conn.ReadFull(buf); err != nil {
				return err
			}
			switch string(buf) {
			case "ping":
				return conn.WriteFull([]byte("pong"))
			case "boom":
				panic("boom")
			default:
				return nil
			}
		}))
	}()

	waitForServer(t, server)

	reply, err := doPipeExchange(listener, []byte("ping"), 4)
	if err != nil {
		t.Fatalf("exchange ping: %v", err)
	}
	if got, want := string(reply), "pong"; got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}

	panicConn, err := listener.Dial()
	if err != nil {
		t.Fatalf("dial panic conn: %v", err)
	}
	defer panicConn.Close()

	if _, err := panicConn.Write([]byte("boom")); err != nil {
		t.Fatalf("write boom: %v", err)
	}
	if err := panicConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	buf := make([]byte, 1)
	_, err = panicConn.Read(buf)
	if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("panic conn read error = %v, want EOF/closed", err)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, "tcp connection panic recovered") {
		t.Fatalf("panic log missing: %q", logOutput)
	}
	if !strings.Contains(logOutput, "stack=") {
		t.Fatalf("panic stack missing: %q", logOutput)
	}
}

func TestServerRejectsSecondStart(t *testing.T) {
	listener := newPipeListener()
	server := tcpx.NewServer(&tcpx.ServerConfig{Listener: listener})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), tcpx.HandlerFunc(func(ctx context.Context, conn tcpx.Connect) error {
			<-ctx.Done()
			return ctx.Err()
		}))
	}()

	waitForServer(t, server)

	err := server.Start(context.Background(), tcpx.HandlerFunc(func(ctx context.Context, conn tcpx.Connect) error {
		return nil
	}))
	if !errors.Is(err, tcpx.ErrServerAlreadyRunning) {
		t.Fatalf("second start error = %v, want %v", err, tcpx.ErrServerAlreadyRunning)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestServerValidation(t *testing.T) {
	server := tcpx.NewServer(nil)

	if err := server.Start(context.Background(), nil); !errors.Is(err, tcpx.ErrNilHandler) {
		t.Fatalf("nil handler error = %v, want %v", err, tcpx.ErrNilHandler)
	}
	if err := server.Serve(context.Background(), nil, tcpx.HandlerFunc(func(ctx context.Context, conn tcpx.Connect) error {
		return nil
	})); !errors.Is(err, tcpx.ErrNilListener) {
		t.Fatalf("nil listener error = %v, want %v", err, tcpx.ErrNilListener)
	}
	if err := server.Start(context.Background(), tcpx.HandlerFunc(func(ctx context.Context, conn tcpx.Connect) error {
		return nil
	})); !errors.Is(err, tcpx.ErrServerAddrRequired) {
		t.Fatalf("missing addr error = %v, want %v", err, tcpx.ErrServerAddrRequired)
	}
}

func TestServerRejectsNilContext(t *testing.T) {
	listener := newPipeListener()
	server := tcpx.NewServer(&tcpx.ServerConfig{Listener: listener})

	var ctx context.Context
	handler := tcpx.HandlerFunc(func(context.Context, tcpx.Connect) error { return nil })

	if err := server.Start(ctx, handler); !errors.Is(err, tcpx.ErrContextRequired) {
		t.Fatalf("Start(nil) error = %v, want %v", err, tcpx.ErrContextRequired)
	}
	if err := server.Serve(ctx, listener, handler); !errors.Is(err, tcpx.ErrContextRequired) {
		t.Fatalf("Serve(nil) error = %v, want %v", err, tcpx.ErrContextRequired)
	}
	if err := server.Shutdown(ctx); !errors.Is(err, tcpx.ErrContextRequired) {
		t.Fatalf("Shutdown(nil) error = %v, want %v", err, tcpx.ErrContextRequired)
	}
}

func TestServerRejectsCanceledStartContext(t *testing.T) {
	listener := newPipeListener()
	server := tcpx.NewServer(&tcpx.ServerConfig{Listener: listener})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := server.Start(ctx, tcpx.HandlerFunc(func(context.Context, tcpx.Connect) error { return nil }))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start(canceled) error = %v, want %v", err, context.Canceled)
	}
}

func TestServerShutdownCancelsHandlerContext(t *testing.T) {
	listener := newPipeListener()
	server := tcpx.NewServer(&tcpx.ServerConfig{
		Listener:        listener,
		ShutdownTimeout: time.Second,
	})

	started := make(chan struct{})
	done := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), tcpx.HandlerFunc(func(ctx context.Context, conn tcpx.Connect) error {
			close(started)
			<-ctx.Done()
			close(done)
			return ctx.Err()
		}))
	}()

	waitForServer(t, server)

	conn, err := listener.Dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler context was not canceled")
	}

	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestServerStopsWhenStartContextCanceled(t *testing.T) {
	listener := newPipeListener()
	server := tcpx.NewServer(&tcpx.ServerConfig{
		Listener:        listener,
		ShutdownTimeout: time.Second,
	})

	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		errCh <- server.Start(ctx, tcpx.HandlerFunc(func(context.Context, tcpx.Connect) error {
			return nil
		}))
	}()

	waitForServer(t, server)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}

func TestServerMaxConnections(t *testing.T) {
	listener := newPipeListener()
	server := tcpx.NewServer(&tcpx.ServerConfig{
		Listener:       listener,
		MaxConnections: 1,
	})

	started := make(chan struct{})
	release := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), tcpx.HandlerFunc(func(ctx context.Context, conn tcpx.Connect) error {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
			return nil
		}))
	}()

	waitForServer(t, server)

	first, err := listener.Dial()
	if err != nil {
		t.Fatalf("dial first: %v", err)
	}
	defer first.Close()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first handler did not start")
	}

	second, err := listener.Dial()
	if err != nil {
		t.Fatalf("dial second: %v", err)
	}
	defer second.Close()

	if err := second.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, err = second.Read(make([]byte, 1))
	if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("second conn read error = %v, want EOF/closed", err)
	}

	close(release)
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestConstructorsCanBeCalledRepeatedly(t *testing.T) {
	_ = tcpx.NewServer(nil)
	_ = tcpx.NewServer(nil)
	_ = tcpx.NewClient(nil)
	_ = tcpx.NewClient(nil)
}

func TestServerMetricsRegistererAndDisableMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = tcpx.NewServer(&tcpx.ServerConfig{
		MetricsRegisterer: reg,
	})

	assertCollectorRegistered(t, reg, prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tcpx_server_connections_active",
			Help: "Current number of active TCP server connections.",
		},
		[]string{"addr"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tcpx_server_connections_accepted_total",
			Help: "Total number of accepted TCP server connections.",
		},
		[]string{"addr"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tcpx_server_connections_closed_total",
			Help: "Total number of closed TCP server connections.",
		},
		[]string{"addr", "result"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tcpx_server_connections_rejected_total",
			Help: "Total number of rejected TCP server connections.",
		},
		[]string{"addr", "reason"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "tcpx_server_connection_duration_seconds",
			Help: "TCP server connection duration in seconds.",
		},
		[]string{"addr", "result"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tcpx_server_bytes_read_total",
			Help: "Total bytes read by the TCP server.",
		},
		[]string{"addr"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tcpx_server_bytes_written_total",
			Help: "Total bytes written by the TCP server.",
		},
		[]string{"addr"},
	))

	disabledReg := prometheus.NewRegistry()
	_ = tcpx.NewServer(&tcpx.ServerConfig{
		DisableMetrics:    true,
		MetricsRegisterer: disabledReg,
	})

	assertCollectorNotRegistered(t, disabledReg, prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tcpx_server_connections_active",
			Help: "Current number of active TCP server connections.",
		},
		[]string{"addr"},
	))
}

func waitForServer(t *testing.T, server tcpx.Server) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if server.Listener() != nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("server did not become ready")
}

type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{
		conns:  make(chan net.Conn, 16),
		closed: make(chan struct{}),
	}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case <-l.closed:
		return nil, net.ErrClosed
	case conn := <-l.conns:
		return conn, nil
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return pipeAddr("pipe")
}

func (l *pipeListener) Dial() (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case <-l.closed:
		_ = client.Close()
		_ = server.Close()
		return nil, net.ErrClosed
	case l.conns <- server:
		return client, nil
	}
}

type pipeAddr string

func (p pipeAddr) Network() string { return string(p) }
func (p pipeAddr) String() string  { return string(p) }

func doPipeExchange(listener *pipeListener, request []byte, replySize int) ([]byte, error) {
	conn, err := listener.Dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if _, err := conn.Write(request); err != nil {
		return nil, err
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return nil, err
	}

	reply := make([]byte, replySize)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return nil, err
	}
	return reply, nil
}
