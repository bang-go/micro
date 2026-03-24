package udpx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Handler interface {
	Handle(context.Context, Packet) error
}

type HandlerFunc func(context.Context, Packet) error

func (f HandlerFunc) Handle(ctx context.Context, packet Packet) error {
	return f(ctx, packet)
}

type Interceptor func(next Handler) Handler

type Server interface {
	Start(context.Context, Handler) error
	Serve(context.Context, net.PacketConn, Handler) error
	Shutdown(context.Context) error
	Close() error
	PacketConn() net.PacketConn
	Use(...Interceptor) error
}

type ServerConfig struct {
	Addr              string
	PacketConn        net.PacketConn
	ReadBuffer        int
	WriteBuffer       int
	MaxPacketSize     int
	MaxConcurrency    int
	ShutdownTimeout   time.Duration
	Logger            *logger.Logger
	EnableLogger      bool
	Trace             bool
	MetricsRegisterer prometheus.Registerer
	DisableMetrics    bool
}

type serverEntity struct {
	config *ServerConfig

	mu           sync.RWMutex
	packetConn   net.PacketConn
	running      bool
	serveCancel  context.CancelCauseFunc
	interceptors []Interceptor
	wg           sync.WaitGroup
	sem          chan struct{}
	metrics      *metrics
}

func NewServer(conf *ServerConfig) Server {
	if conf == nil {
		conf = &ServerConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}
	if conf.MaxPacketSize <= 0 {
		conf.MaxPacketSize = defaultServerMaxPacketSize
	}
	if conf.MaxConcurrency == 0 {
		conf.MaxConcurrency = defaultServerMaxConcurrency
	}
	if conf.MaxConcurrency < 0 {
		conf.MaxConcurrency = 0
	}
	if conf.ShutdownTimeout == 0 {
		conf.ShutdownTimeout = defaultServerShutdownTime
	}

	var metrics *metrics
	if !conf.DisableMetrics {
		metrics = defaultUDPMetrics()
		if conf.MetricsRegisterer != nil {
			metrics = newUDPMetrics(conf.MetricsRegisterer)
		}
	}

	return &serverEntity{
		config:  conf,
		metrics: metrics,
	}
}

func (s *serverEntity) Use(interceptors ...Interceptor) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return ErrServerAlreadyRunning
	}
	for _, interceptor := range interceptors {
		if interceptor == nil {
			continue
		}
		s.interceptors = append(s.interceptors, interceptor)
	}
	return nil
}

func (s *serverEntity) Start(ctx context.Context, handler Handler) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if handler == nil {
		return ErrNilHandler
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.config.PacketConn != nil {
		return s.Serve(ctx, s.config.PacketConn, handler)
	}
	if s.config.Addr == "" {
		return ErrServerAddrRequired
	}

	packetConn, err := net.ListenPacket("udp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("udpx: listen: %w", err)
	}
	err = s.Serve(ctx, packetConn, handler)
	if err != nil {
		_ = packetConn.Close()
	}
	return err
}

func (s *serverEntity) Serve(ctx context.Context, packetConn net.PacketConn, handler Handler) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if packetConn == nil {
		return ErrNilPacketConn
	}
	if handler == nil {
		return ErrNilHandler
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrServerAlreadyRunning
	}

	serveCtx, serveCancel := context.WithCancelCause(ctx)
	s.packetConn = packetConn
	s.serveCancel = serveCancel
	s.running = true
	if s.config.MaxConcurrency > 0 {
		s.sem = make(chan struct{}, s.config.MaxConcurrency)
	} else {
		s.sem = nil
	}
	interceptors := append([]Interceptor(nil), s.interceptors...)
	s.mu.Unlock()

	defer func() {
		serveCancel(errServerClosed)
		s.mu.Lock()
		s.packetConn = nil
		s.serveCancel = nil
		s.running = false
		s.sem = nil
		s.mu.Unlock()
	}()

	if err := s.configurePacketConn(packetConn); err != nil {
		return err
	}

	done := make(chan struct{})
	defer close(done)
	go s.watchContext(ctx, done)

	finalHandler := chainInterceptors(handler, interceptors)
	addrLabel := ""
	if localAddr := packetConn.LocalAddr(); localAddr != nil {
		addrLabel = localAddr.String()
	}
	tracer := otel.Tracer("micro/udpx")
	var bufPool = sync.Pool{
		New: func() any {
			return make([]byte, s.config.MaxPacketSize)
		},
	}

	s.info(ctx, "udp server starting", "addr", addrLabel)

	for {
		buffer := bufPool.Get().([]byte)
		n, remoteAddr, truncated, err := s.readPacket(packetConn, buffer)
		if err != nil {
			bufPool.Put(buffer)
			if isClosedConnectionError(err) || errors.Is(context.Cause(serveCtx), errServerClosed) {
				s.stopActivePackets(errServerClosed)
				s.wg.Wait()
				return nil
			}
			if isTemporaryNetError(err) {
				s.config.Logger.Warn(ctx, "udp read temporary failure", "error", err)
				continue
			}

			readErr := fmt.Errorf("udpx: read packet: %w", err)
			s.stopActivePackets(readErr)
			s.wg.Wait()
			return readErr
		}

		if s.metrics != nil {
			s.metrics.serverPacketsReceived.WithLabelValues(addrLabel).Inc()
			s.metrics.serverBytesRead.WithLabelValues(addrLabel).Add(float64(n))
		}

		if truncated {
			if s.metrics != nil {
				s.metrics.serverPacketsDropped.WithLabelValues(addrLabel, "truncated").Inc()
			}
			bufPool.Put(buffer)
			s.config.Logger.Warn(ctx, "udp packet dropped", "reason", "truncated", "remote_addr", remoteAddr.String(), "max_packet_size", s.config.MaxPacketSize)
			continue
		}

		payload := append([]byte(nil), buffer[:n]...)
		bufPool.Put(buffer)

		if err := s.acquireSlot(serveCtx); err != nil {
			return nil
		}

		var onWrite func(int)
		if s.metrics != nil {
			onWrite = func(written int) {
				s.metrics.serverBytesWritten.WithLabelValues(addrLabel).Add(float64(written))
			}
		}
		packet := newPacket(payload, remoteAddr, packetConn.LocalAddr(), packetConn, onWrite)

		s.wg.Add(1)
		go s.handlePacket(serveCtx, tracer, addrLabel, finalHandler, packet)
	}
}

func (s *serverEntity) Shutdown(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if s.config.ShutdownTimeout > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, s.config.ShutdownTimeout)
			defer cancel()
		}
	}

	packetConn, cancel := s.snapshotState()
	if packetConn == nil && cancel == nil {
		return nil
	}

	s.info(ctx, "udp server shutting down")

	if cancel != nil {
		cancel(errServerClosed)
	}
	if packetConn != nil {
		_ = packetConn.Close()
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		closeErr := s.Close()
		if closeErr != nil {
			return errors.Join(ctx.Err(), closeErr)
		}
		return ctx.Err()
	}
}

func (s *serverEntity) Close() error {
	packetConn, cancel := s.snapshotState()
	if cancel != nil {
		cancel(errServerClosed)
	}
	if packetConn == nil {
		return nil
	}
	err := packetConn.Close()
	if isClosedConnectionError(err) {
		return nil
	}
	return err
}

func (s *serverEntity) PacketConn() net.PacketConn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.packetConn
}

func (s *serverEntity) snapshotState() (net.PacketConn, context.CancelCauseFunc) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.packetConn, s.serveCancel
}

func (s *serverEntity) stopActivePackets(cause error) {
	packetConn, cancel := s.snapshotState()
	if cancel != nil {
		cancel(cause)
	}
	if packetConn != nil {
		_ = packetConn.Close()
	}
}

func (s *serverEntity) acquireSlot(ctx context.Context) error {
	s.mu.RLock()
	sem := s.sem
	s.mu.RUnlock()

	if sem == nil {
		return nil
	}

	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *serverEntity) releaseSlot() {
	s.mu.RLock()
	sem := s.sem
	s.mu.RUnlock()

	if sem == nil {
		return
	}
	<-sem
}

func (s *serverEntity) handlePacket(
	ctx context.Context,
	tracer trace.Tracer,
	addrLabel string,
	handler Handler,
	packet Packet,
) {
	defer s.wg.Done()
	defer s.releaseSlot()

	packetCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var span trace.Span
	if s.config.Trace {
		packetCtx, span = tracer.Start(packetCtx, "udpx.server.packet",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(connectionAttributes(packet.LocalAddr(), packet.RemoteAddr())...),
		)
		defer span.End()
	}

	if s.config.EnableLogger {
		s.config.Logger.Debug(packetCtx, "udp packet received",
			"remote_addr", packet.RemoteAddr().String(),
			"size", len(packet.Payload()),
		)
	}

	start := time.Now()
	var (
		handleErr error
		panicked  bool
	)

	defer func() {
		if recovered := recover(); recovered != nil {
			panicked = true
			handleErr = fmt.Errorf("panic: %v", recovered)
			if span != nil {
				span.RecordError(handleErr)
				span.SetStatus(codes.Error, handleErr.Error())
			}
			s.config.Logger.Error(packetCtx, "udp packet panic recovered",
				"remote_addr", packet.RemoteAddr().String(),
				"panic", recovered,
				"stack", string(debug.Stack()),
			)
		}

		result := classifyPacketResult(packetCtx, handleErr, panicked)
		duration := time.Since(start)
		if s.metrics != nil {
			s.metrics.serverPacketsHandled.WithLabelValues(addrLabel, result).Inc()
			s.metrics.serverPacketDuration.WithLabelValues(addrLabel, result).Observe(duration.Seconds())
		}

		switch result {
		case "error":
			if span != nil {
				span.RecordError(handleErr)
				span.SetStatus(codes.Error, handleErr.Error())
			}
			s.config.Logger.Error(packetCtx, "udp packet handling failed",
				"remote_addr", packet.RemoteAddr().String(),
				"duration", duration,
				"error", handleErr,
			)
		case "panic":
		default:
			if s.config.EnableLogger {
				s.config.Logger.Debug(packetCtx, "udp packet handled",
					"remote_addr", packet.RemoteAddr().String(),
					"duration", duration,
					"result", result,
				)
			}
		}
	}()

	handleErr = handler.Handle(packetCtx, packet)
}

func (s *serverEntity) configurePacketConn(packetConn net.PacketConn) error {
	if s.config.ReadBuffer > 0 {
		if setter, ok := packetConn.(interface{ SetReadBuffer(int) error }); ok {
			if err := setter.SetReadBuffer(s.config.ReadBuffer); err != nil {
				return err
			}
		}
	}
	if s.config.WriteBuffer > 0 {
		if setter, ok := packetConn.(interface{ SetWriteBuffer(int) error }); ok {
			if err := setter.SetWriteBuffer(s.config.WriteBuffer); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *serverEntity) readPacket(packetConn net.PacketConn, buffer []byte) (int, net.Addr, bool, error) {
	type messageReader interface {
		ReadMsgUDP([]byte, []byte) (int, int, int, *net.UDPAddr, error)
	}
	if reader, ok := packetConn.(messageReader); ok {
		n, _, flags, addr, err := reader.ReadMsgUDP(buffer, nil)
		if err != nil {
			return 0, nil, false, err
		}
		return n, addr, flags&syscall.MSG_TRUNC != 0, nil
	}

	n, addr, err := packetConn.ReadFrom(buffer)
	return n, addr, false, err
}

func (s *serverEntity) watchContext(ctx context.Context, done <-chan struct{}) {
	select {
	case <-done:
	case <-ctx.Done():
		_ = s.Shutdown(context.Background())
	}
}

func (s *serverEntity) info(ctx context.Context, msg string, args ...any) {
	if s.config.EnableLogger {
		s.config.Logger.Info(normalizeContext(ctx), msg, args...)
	}
}

func chainInterceptors(handler Handler, interceptors []Interceptor) Handler {
	finalHandler := handler
	for i := len(interceptors) - 1; i >= 0; i-- {
		if interceptors[i] == nil {
			continue
		}
		finalHandler = interceptors[i](finalHandler)
	}
	return finalHandler
}

func connectionAttributes(local, remote net.Addr) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("network.transport", "udp"),
	}

	localHost, localPort := splitAddr(local)
	remoteHost, remotePort := splitAddr(remote)

	if localHost != "" {
		attrs = append(attrs, attribute.String("server.address", localHost))
	}
	if localPort > 0 {
		attrs = append(attrs, attribute.Int("server.port", localPort))
	}
	if remoteHost != "" {
		attrs = append(attrs, attribute.String("client.address", remoteHost))
	}
	if remotePort > 0 {
		attrs = append(attrs, attribute.Int("client.port", remotePort))
	}

	return attrs
}
