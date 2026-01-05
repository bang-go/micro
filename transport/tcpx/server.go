package tcpx

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/bang-go/micro/pkg/pool"
	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Prometheus Metrics
var (
	ServerConnections = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tcpx_server_connections_active",
			Help: "Current number of active TCP connections",
		},
		[]string{"addr"},
	)
	ServerBytesReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tcpx_server_bytes_received_total",
			Help: "Total bytes received by TCP server",
		},
		[]string{"addr"},
	)
	ServerBytesSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tcpx_server_bytes_sent_total",
			Help: "Total bytes sent by TCP server",
		},
		[]string{"addr"},
	)
)

func init() {
	prometheus.MustRegister(ServerConnections)
	prometheus.MustRegister(ServerBytesReceived)
	prometheus.MustRegister(ServerBytesSent)
}

// Handler handles a new connection.
// It is responsible for reading from and writing to the connection.
type Handler interface {
	Handle(ctx context.Context, conn Connect) error
}

// HandlerFunc is an adapter to allow the use of ordinary functions as handlers.
type HandlerFunc func(ctx context.Context, conn Connect) error

func (f HandlerFunc) Handle(ctx context.Context, conn Connect) error {
	return f(ctx, conn)
}

// Interceptor defines a hook for connection handling
type Interceptor func(next Handler) Handler

type Server interface {
	Start(Handler) error
	Shutdown(context.Context) error
	Use(interceptors ...Interceptor)
}

type ServerConfig struct {
	Addr         string
	Timeout      time.Duration // Idle timeout for connections
	MaxConns     int           // Max concurrent connections. If > 0, connection pool is used.
	Trace        bool
	Logger       *logger.Logger
	EnableLogger bool
}

type serverEntity struct {
	config       *ServerConfig
	listen       *net.TCPListener
	stopCh       chan struct{}
	isRunning    bool
	mu           sync.Mutex
	wg           sync.WaitGroup // For graceful shutdown
	workerPool   pool.Pool      // Optional worker pool for connections
	interceptors []Interceptor
}

func NewServer(conf *ServerConfig) Server {
	if conf == nil {
		conf = &ServerConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}
	if conf.MaxConns <= 0 {
		conf.MaxConns = 10000 // Default max connections if used for pool
	}

	return &serverEntity{
		config:    conf,
		stopCh:    make(chan struct{}),
		isRunning: false,
	}
}

func (s *serverEntity) Use(interceptors ...Interceptor) {
	s.interceptors = append(s.interceptors, interceptors...)
}

func (s *serverEntity) Start(handler Handler) (err error) {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return errors.New("server is already running")
	}

	tcpAddr, err := net.ResolveTCPAddr("tcp", s.config.Addr)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.listen, err = net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		s.mu.Unlock()
		return err
	}

	// Initialize Worker Pool if MaxConns > 0 (effectively always true with default)
	// We use NonBlocking=false by default (Accept blocks when pool full),
	// but standard TCP server usually accepts and closes if full, or blocks Accept.
	// Blocking Accept is fine as it applies backpressure to OS backlog.
	s.workerPool, err = pool.New(s.config.MaxConns,
		pool.WithLogger(s.config.Logger),
		pool.WithQueueSize(0), // Unbuffered? No, pool uses buffered chan as queue. Size 0 defaults to capacity.
	)
	if err != nil {
		s.mu.Unlock()
		return err
	}

	s.isRunning = true
	stopCh := s.stopCh
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isRunning = false
		s.mu.Unlock()
	}()

	// Chain interceptors
	finalHandler := handler
	for i := len(s.interceptors) - 1; i >= 0; i-- {
		finalHandler = s.interceptors[i](finalHandler)
	}

	tracer := otel.Tracer("micro/tcpx")

	var tempDelay time.Duration // how long to sleep on accept failure

	for {
		select {
		case <-stopCh:
			return nil
		default:
			conn, err := s.listen.Accept()
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && !opErr.Temporary() {
					// Listener closed
					return nil
				}
				if ne, ok := err.(net.Error); ok && ne.Temporary() {
					if tempDelay == 0 {
						tempDelay = 5 * time.Millisecond
					} else {
						tempDelay *= 2
					}
					if maxDelay := 1 * time.Second; tempDelay > maxDelay {
						tempDelay = maxDelay
					}
					s.config.Logger.Warn(context.Background(), "tcpx_accept_temp_error", "error", err, "retry_in", tempDelay)
					time.Sleep(tempDelay)
					continue
				}
				// Fatal error
				s.config.Logger.Error(context.Background(), "tcpx_accept_fatal_error", "error", err)
				return err
			}
			tempDelay = 0

			// Dispatch to pool
			// Accept blocks if pool is full.
			// This is good for backpressure.
			err = s.workerPool.Submit(func() {
				s.handleConn(conn, finalHandler, tracer)
			})

			if err != nil {
				// Pool closed or error
				s.config.Logger.Error(context.Background(), "tcpx_submit_error", "error", err)
				conn.Close() // Close connection if cannot submit
			}
		}
	}
}

func (s *serverEntity) handleConn(conn net.Conn, handler Handler, tracer trace.Tracer) {
	// Metrics
	ServerConnections.WithLabelValues(s.config.Addr).Inc()
	defer ServerConnections.WithLabelValues(s.config.Addr).Dec()

	// Context with Trace
	ctx := context.Background()
	if s.config.Trace {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "tcp.Handle",
			trace.WithAttributes(
				attribute.String("net.peer.ip", conn.RemoteAddr().String()),
				attribute.String("net.transport", "tcp"),
			),
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()
	}

	// Wrapper connection for stats or timeout
	wrappedConn := NewConnect(conn, WithConnectTimeout(s.config.Timeout))

	defer func() {
		// Panic recovery is done by pool, but we need to ensure conn is closed.
		// Pool recovers panic, but doesn't know about cleanup like Close().
		// So we MUST keep this defer for cleanup.
		wrappedConn.Close()
	}()

	if s.config.EnableLogger {
		s.config.Logger.Info(ctx, "tcpx_conn_start", "remote", conn.RemoteAddr().String())
	}

	start := time.Now()
	err := handler.Handle(ctx, wrappedConn)
	duration := time.Since(start)

	if s.config.EnableLogger {
		if err != nil {
			s.config.Logger.Error(ctx, "tcpx_conn_error", "remote", conn.RemoteAddr().String(), "error", err, "cost", duration.Seconds())
		} else {
			s.config.Logger.Info(ctx, "tcpx_conn_end", "remote", conn.RemoteAddr().String(), "cost", duration.Seconds())
		}
	}
}

func (s *serverEntity) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.isRunning {
		s.mu.Unlock()
		return nil
	}
	s.isRunning = false
	close(s.stopCh)
	// Close listener to unblock Accept
	if s.listen != nil {
		s.listen.Close()
	}
	s.mu.Unlock()

	// Wait for pool
	done := make(chan struct{})
	go func() {
		if s.workerPool != nil {
			s.workerPool.Release()
		}
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
