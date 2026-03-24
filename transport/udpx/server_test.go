package udpx_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/micro/transport/udpx"
	"github.com/prometheus/client_golang/prometheus"
)

func TestServerServeShutdownAndRecovery(t *testing.T) {
	packetConn := newFakePacketConn()

	var logs safeBuffer
	server := udpx.NewServer(&udpx.ServerConfig{
		PacketConn:    packetConn,
		EnableLogger:  true,
		MaxPacketSize: 1024,
		Logger: logger.New(
			logger.WithFormat("text"),
			logger.WithAddSource(false),
			logger.WithOutput(&logs),
		),
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
			switch string(packet.Payload()) {
			case "ping":
				_, err := packet.Reply(ctx, []byte("pong"))
				return err
			case "boom":
				panic("boom")
			default:
				return nil
			}
		}))
	}()

	waitForServer(t, server)

	remoteAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30001}
	packetConn.enqueuePacket([]byte("ping"), remoteAddr)

	reply, err := packetConn.nextWrite(time.Second)
	if err != nil {
		t.Fatalf("next write: %v", err)
	}
	if got, want := string(reply.data), "pong"; got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
	if got, want := reply.addr.String(), remoteAddr.String(); got != want {
		t.Fatalf("reply addr = %q, want %q", got, want)
	}

	packetConn.enqueuePacket([]byte("boom"), remoteAddr)
	waitForLog(t, &logs, "udp packet panic recovered")

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, "udp packet panic recovered") {
		t.Fatalf("panic log missing: %q", logOutput)
	}
	if !strings.Contains(logOutput, "stack=") {
		t.Fatalf("panic stack missing: %q", logOutput)
	}
}

func TestServerRejectsSecondStart(t *testing.T) {
	packetConn := newFakePacketConn()
	server := udpx.NewServer(&udpx.ServerConfig{PacketConn: packetConn})

	started := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
			select {
			case <-started:
			default:
				close(started)
			}
			<-ctx.Done()
			return ctx.Err()
		}))
	}()

	waitForServer(t, server)
	packetConn.enqueuePacket([]byte("hello"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30002})

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}

	err := server.Start(context.Background(), udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
		return nil
	}))
	if !errors.Is(err, udpx.ErrServerAlreadyRunning) {
		t.Fatalf("second start error = %v, want %v", err, udpx.ErrServerAlreadyRunning)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestServerUseInterceptorLifecycle(t *testing.T) {
	packetConn := newFakePacketConn()
	server := udpx.NewServer(&udpx.ServerConfig{PacketConn: packetConn})

	intercepted := make(chan struct{}, 1)
	err := server.Use(func(next udpx.Handler) udpx.Handler {
		return udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
			select {
			case intercepted <- struct{}{}:
			default:
			}
			return next.Handle(ctx, packet)
		})
	})
	if err != nil {
		t.Fatalf("Use before start: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
			return nil
		}))
	}()

	waitForServer(t, server)
	packetConn.enqueuePacket([]byte("hello"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30005})

	select {
	case <-intercepted:
	case <-time.After(time.Second):
		t.Fatal("interceptor was not invoked")
	}

	err = server.Use(func(next udpx.Handler) udpx.Handler { return next })
	if !errors.Is(err, udpx.ErrServerAlreadyRunning) {
		t.Fatalf("Use while running error = %v, want %v", err, udpx.ErrServerAlreadyRunning)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestServerValidation(t *testing.T) {
	server := udpx.NewServer(nil)

	if err := server.Start(nil, udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
		return nil
	})); !errors.Is(err, udpx.ErrContextRequired) {
		t.Fatalf("nil start context error = %v, want %v", err, udpx.ErrContextRequired)
	}
	if err := server.Serve(nil, newFakePacketConn(), udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
		return nil
	})); !errors.Is(err, udpx.ErrContextRequired) {
		t.Fatalf("nil serve context error = %v, want %v", err, udpx.ErrContextRequired)
	}
	if err := server.Shutdown(nil); !errors.Is(err, udpx.ErrContextRequired) {
		t.Fatalf("nil shutdown context error = %v, want %v", err, udpx.ErrContextRequired)
	}
	if err := server.Start(context.Background(), nil); !errors.Is(err, udpx.ErrNilHandler) {
		t.Fatalf("nil handler error = %v, want %v", err, udpx.ErrNilHandler)
	}
	if err := server.Serve(context.Background(), nil, udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
		return nil
	})); !errors.Is(err, udpx.ErrNilPacketConn) {
		t.Fatalf("nil packet conn error = %v, want %v", err, udpx.ErrNilPacketConn)
	}
	if err := server.Start(context.Background(), udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
		return nil
	})); !errors.Is(err, udpx.ErrServerAddrRequired) {
		t.Fatalf("missing addr error = %v, want %v", err, udpx.ErrServerAddrRequired)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := server.Start(ctx, udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
		return nil
	})); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled start context error = %v, want %v", err, context.Canceled)
	}
	if err := server.Serve(ctx, newFakePacketConn(), udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
		return nil
	})); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled serve context error = %v, want %v", err, context.Canceled)
	}
}

func TestServerShutdownCancelsHandlerContext(t *testing.T) {
	packetConn := newFakePacketConn()
	server := udpx.NewServer(&udpx.ServerConfig{
		PacketConn:      packetConn,
		ShutdownTimeout: time.Second,
	})

	started := make(chan struct{})
	done := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
			close(started)
			<-ctx.Done()
			close(done)
			return ctx.Err()
		}))
	}()

	waitForServer(t, server)
	packetConn.enqueuePacket([]byte("hello"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30003})

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

func TestServerDropsTruncatedPackets(t *testing.T) {
	packetConn := newFakePacketConn()

	var logs safeBuffer
	server := udpx.NewServer(&udpx.ServerConfig{
		PacketConn:    packetConn,
		MaxPacketSize: 4,
		Logger: logger.New(
			logger.WithFormat("text"),
			logger.WithAddSource(false),
			logger.WithOutput(&logs),
		),
	})

	handled := make(chan struct{}, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
			handled <- struct{}{}
			return nil
		}))
	}()

	waitForServer(t, server)
	packetConn.enqueueTruncatedPacket([]byte("toolong"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30004})

	select {
	case <-handled:
		t.Fatal("truncated packet should not be delivered to handler")
	case <-time.After(100 * time.Millisecond):
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}

	if !strings.Contains(logs.String(), "reason=truncated") {
		t.Fatalf("truncated packet log missing: %q", logs.String())
	}
}

func TestConstructorsCanBeCalledRepeatedly(t *testing.T) {
	_ = udpx.NewServer(nil)
	_ = udpx.NewServer(nil)
	_ = udpx.NewClient(nil)
	_ = udpx.NewClient(nil)
}

func TestServerStopsWhenStartContextCanceled(t *testing.T) {
	packetConn := newFakePacketConn()
	server := udpx.NewServer(&udpx.ServerConfig{
		PacketConn: packetConn,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx, udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
			return nil
		}))
	}()

	waitForServer(t, server)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("start returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop after start context cancellation")
	}

	if server.PacketConn() != nil {
		t.Fatal("server packet conn should be cleared after stop")
	}
}

func TestPacketReplyRequiresContext(t *testing.T) {
	packetConn := newFakePacketConn()
	server := udpx.NewServer(&udpx.ServerConfig{
		PacketConn: packetConn,
	})

	packetErrCh := make(chan error, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
			_, err := packet.Reply(nil, []byte("pong"))
			packetErrCh <- err
			return nil
		}))
	}()

	waitForServer(t, server)
	packetConn.enqueuePacket([]byte("ping"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30006})

	select {
	case err := <-packetErrCh:
		if !errors.Is(err, udpx.ErrContextRequired) {
			t.Fatalf("reply error = %v, want %v", err, udpx.ErrContextRequired)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not return packet write result")
	}

	if _, err := packetConn.nextWrite(100 * time.Millisecond); err == nil {
		t.Fatal("packet write should not happen when context is nil")
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestServerMetricsRegistererAndDisableMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = udpx.NewServer(&udpx.ServerConfig{
		MetricsRegisterer: reg,
	})

	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "udpx_server_packets_received_total",
			Help: "Total number of UDP packets read by the server.",
		},
		[]string{"addr"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "udpx_server_packets_handled_total",
			Help: "Total number of UDP packets processed by the server.",
		},
		[]string{"addr", "result"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "udpx_server_packets_dropped_total",
			Help: "Total number of UDP packets dropped by the server.",
		},
		[]string{"addr", "reason"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "udpx_server_packet_duration_seconds",
			Help: "UDP packet handling duration in seconds.",
		},
		[]string{"addr", "result"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "udpx_server_bytes_read_total",
			Help: "Total bytes read by the UDP server.",
		},
		[]string{"addr"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "udpx_server_bytes_written_total",
			Help: "Total bytes written by the UDP server.",
		},
		[]string{"addr"},
	))

	disabledReg := prometheus.NewRegistry()
	_ = udpx.NewServer(&udpx.ServerConfig{
		DisableMetrics:    true,
		MetricsRegisterer: disabledReg,
	})

	assertCollectorNotRegistered(t, disabledReg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "udpx_server_packets_received_total",
			Help: "Total number of UDP packets read by the server.",
		},
		[]string{"addr"},
	))
}

func waitForServer(t *testing.T, server udpx.Server) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if server.PacketConn() != nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("server did not become ready")
}

func waitForLog(t *testing.T, logs *safeBuffer, pattern string) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logs.String(), pattern) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("log %q not found in %q", pattern, logs.String())
}
