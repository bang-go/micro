package tcpx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Handler interface {
	Handle(context.Context, Connect) error
}

type HandlerFunc func(context.Context, Connect) error

func (f HandlerFunc) Handle(ctx context.Context, conn Connect) error {
	return f(ctx, conn)
}

type Interceptor func(next Handler) Handler

type Server interface {
	Start(context.Context, Handler) error
	Serve(context.Context, net.Listener, Handler) error
	Shutdown(context.Context) error
	Close() error
	Listener() net.Listener
	Use(...Interceptor) error
}

type ServerConfig struct {
	Addr              string
	Listener          net.Listener
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	KeepAlivePeriod   time.Duration
	ShutdownTimeout   time.Duration
	MaxConnections    int
	TLSConfig         *tls.Config
	Logger            *logger.Logger
	EnableLogger      bool
	Trace             bool
	MetricsRegisterer prometheus.Registerer
	DisableMetrics    bool
}

type serverEntity struct {
	config *ServerConfig

	mu           sync.RWMutex
	listener     net.Listener
	running      bool
	serveCancel  context.CancelCauseFunc
	connections  map[Connect]struct{}
	interceptors []Interceptor
	wg           sync.WaitGroup
	metrics      *metrics
}

func NewServer(conf *ServerConfig) Server {
	if conf == nil {
		conf = &ServerConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}
	if conf.KeepAlivePeriod == 0 {
		conf.KeepAlivePeriod = defaultServerKeepAlive
	}
	if conf.ShutdownTimeout == 0 {
		conf.ShutdownTimeout = defaultServerShutdownTime
	}
	if conf.MaxConnections < 0 {
		conf.MaxConnections = 0
	}

	var metrics *metrics
	if !conf.DisableMetrics {
		metrics = defaultTCPMetrics()
		if conf.MetricsRegisterer != nil {
			metrics = newTCPMetrics(conf.MetricsRegisterer)
		}
	}

	return &serverEntity{
		config:      conf,
		connections: make(map[Connect]struct{}),
		metrics:     metrics,
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
	if s.config.Listener != nil {
		return s.Serve(ctx, s.config.Listener, handler)
	}
	if s.config.Addr == "" {
		return ErrServerAddrRequired
	}

	listener, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("tcpx: listen: %w", err)
	}
	err = s.Serve(ctx, listener, handler)
	if err != nil {
		_ = listener.Close()
	}
	return err
}

func (s *serverEntity) Serve(ctx context.Context, listener net.Listener, handler Handler) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if listener == nil {
		return ErrNilListener
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
	activeListener := listener
	if s.config.TLSConfig != nil {
		activeListener = tls.NewListener(listener, s.config.TLSConfig.Clone())
	}

	s.listener = activeListener
	s.serveCancel = serveCancel
	s.running = true
	s.connections = make(map[Connect]struct{})
	interceptors := append([]Interceptor(nil), s.interceptors...)
	s.mu.Unlock()

	defer func() {
		serveCancel(errServerClosed)
		s.mu.Lock()
		s.listener = nil
		s.serveCancel = nil
		s.running = false
		s.connections = make(map[Connect]struct{})
		s.mu.Unlock()
	}()

	done := make(chan struct{})
	defer close(done)
	go s.watchContext(ctx, done)

	finalHandler := chainInterceptors(handler, interceptors)
	s.info(ctx, "tcp server starting", "addr", activeListener.Addr().String())

	addrLabel := activeListener.Addr().String()
	tracer := otel.Tracer("micro/tcpx")
	var tempDelay time.Duration

	for {
		rawConn, err := activeListener.Accept()
		if err != nil {
			if isListenerClosedError(err) || errors.Is(context.Cause(serveCtx), errServerClosed) {
				s.wg.Wait()
				return nil
			}

			if isTemporaryNetError(err) {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if tempDelay > time.Second {
					tempDelay = time.Second
				}
				s.config.Logger.Warn(ctx, "tcp accept temporary failure", "error", err, "retry_in", tempDelay)
				time.Sleep(tempDelay)
				continue
			}

			acceptErr := fmt.Errorf("tcpx: accept: %w", err)
			s.stopActiveConnections(acceptErr)
			s.wg.Wait()
			return acceptErr
		}
		tempDelay = 0

		if err := s.configureSocket(rawConn); err != nil {
			s.config.Logger.Warn(ctx, "tcp socket configuration failed", "error", err, "remote_addr", rawConn.RemoteAddr().String())
			_ = rawConn.Close()
			continue
		}

		conn, err := NewConnect(rawConn,
			WithReadTimeout(s.config.ReadTimeout),
			WithWriteTimeout(s.config.WriteTimeout),
			withReadObserver(func(n int) {
				if s.metrics != nil {
					s.metrics.serverBytesRead.WithLabelValues(addrLabel).Add(float64(n))
				}
			}),
			withWriteObserver(func(n int) {
				if s.metrics != nil {
					s.metrics.serverBytesWritten.WithLabelValues(addrLabel).Add(float64(n))
				}
			}),
		)
		if err != nil {
			_ = rawConn.Close()
			return err
		}

		if err := s.registerConnection(serveCtx, conn, addrLabel); err != nil {
			_ = conn.Close()
			reason := "server_closed"
			if errors.Is(err, errMaxConnectionsReached) {
				reason = "max_connections"
			}
			if s.metrics != nil {
				s.metrics.serverConnectionsRejected.WithLabelValues(addrLabel, reason).Inc()
			}
			if errors.Is(err, errMaxConnectionsReached) {
				s.config.Logger.Warn(ctx, "tcp connection rejected", "remote_addr", conn.RemoteAddr().String(), "reason", reason)
			}
			continue
		}

		s.wg.Add(1)
		go s.handleConnection(serveCtx, tracer, addrLabel, finalHandler, conn)
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

	listener, cancel, connections := s.snapshotState()
	if listener == nil && cancel == nil {
		return nil
	}

	s.info(ctx, "tcp server shutting down")

	if cancel != nil {
		cancel(errServerClosed)
	}
	if listener != nil {
		_ = listener.Close()
	}
	for _, conn := range connections {
		_ = conn.Close()
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
	listener, cancel, connections := s.snapshotState()

	if cancel != nil {
		cancel(errServerClosed)
	}

	var errs []error
	if listener != nil {
		if err := listener.Close(); err != nil && !isListenerClosedError(err) {
			errs = append(errs, err)
		}
	}
	for _, conn := range connections {
		if err := conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *serverEntity) Listener() net.Listener {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listener
}

func (s *serverEntity) registerConnection(ctx context.Context, conn Connect, addrLabel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if context.Cause(ctx) != nil {
		return errServerClosed
	}
	if s.config.MaxConnections > 0 && len(s.connections) >= s.config.MaxConnections {
		return errMaxConnectionsReached
	}

	s.connections[conn] = struct{}{}
	if s.metrics != nil {
		s.metrics.serverConnectionsActive.WithLabelValues(addrLabel).Inc()
		s.metrics.serverConnectionsAccepted.WithLabelValues(addrLabel).Inc()
	}
	return nil
}

func (s *serverEntity) unregisterConnection(conn Connect, addrLabel string) {
	s.mu.Lock()
	delete(s.connections, conn)
	s.mu.Unlock()
	if s.metrics != nil {
		s.metrics.serverConnectionsActive.WithLabelValues(addrLabel).Dec()
	}
}

func (s *serverEntity) snapshotState() (net.Listener, context.CancelCauseFunc, []Connect) {
	s.mu.Lock()
	defer s.mu.Unlock()

	connections := make([]Connect, 0, len(s.connections))
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	return s.listener, s.serveCancel, connections
}

func (s *serverEntity) stopActiveConnections(cause error) {
	listener, cancel, connections := s.snapshotState()
	if cancel != nil {
		cancel(cause)
	}
	if listener != nil {
		_ = listener.Close()
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
}

func (s *serverEntity) handleConnection(
	ctx context.Context,
	tracer trace.Tracer,
	addrLabel string,
	handler Handler,
	conn Connect,
) {
	defer s.wg.Done()
	defer s.unregisterConnection(conn, addrLabel)

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	localAddr := ""
	remoteAddr := ""
	if conn.LocalAddr() != nil {
		localAddr = conn.LocalAddr().String()
	}
	if conn.RemoteAddr() != nil {
		remoteAddr = conn.RemoteAddr().String()
	}

	var span trace.Span
	if s.config.Trace {
		connCtx, span = tracer.Start(connCtx, "tcpx.server.connection",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(connectionAttributes(conn)...),
		)
		defer span.End()
	}

	if s.config.EnableLogger {
		s.config.Logger.Info(connCtx, "tcp connection accepted", "local_addr", localAddr, "remote_addr", remoteAddr)
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
			s.config.Logger.Error(connCtx, "tcp connection panic recovered",
				"local_addr", localAddr,
				"remote_addr", remoteAddr,
				"panic", recovered,
				"stack", string(debug.Stack()),
			)
		}

		_ = conn.Close()

		result := classifyConnectionResult(ctx, handleErr, panicked)
		duration := time.Since(start)
		if s.metrics != nil {
			s.metrics.serverConnectionsClosed.WithLabelValues(addrLabel, result).Inc()
			s.metrics.serverConnectionDuration.WithLabelValues(addrLabel, result).Observe(duration.Seconds())
		}

		switch result {
		case "error":
			if span != nil {
				span.RecordError(handleErr)
				span.SetStatus(codes.Error, handleErr.Error())
			}
			s.config.Logger.Error(connCtx, "tcp connection failed",
				"local_addr", localAddr,
				"remote_addr", remoteAddr,
				"duration", duration,
				"error", handleErr,
			)
		case "panic":
			s.info(connCtx, "tcp connection closed", "local_addr", localAddr, "remote_addr", remoteAddr, "duration", duration, "result", result)
		default:
			s.info(connCtx, "tcp connection closed", "local_addr", localAddr, "remote_addr", remoteAddr, "duration", duration, "result", result)
		}
	}()

	handleErr = handler.Handle(connCtx, conn)
}

func (s *serverEntity) configureSocket(conn net.Conn) error {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil
	}
	if s.config.KeepAlivePeriod <= 0 {
		return nil
	}
	if err := tcpConn.SetKeepAlive(true); err != nil {
		return err
	}
	if err := tcpConn.SetKeepAlivePeriod(s.config.KeepAlivePeriod); err != nil {
		return err
	}
	return nil
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

func connectionAttributes(conn Connect) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("network.transport", "tcp"),
	}

	localHost, localPort := splitAddr(conn.LocalAddr())
	remoteHost, remotePort := splitAddr(conn.RemoteAddr())

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
