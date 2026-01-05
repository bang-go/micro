package udpx

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
	ServerPacketsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "udpx_server_packets_received_total",
			Help: "Total packets received by UDP server",
		},
		[]string{"addr"},
	)
	ServerPacketsSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "udpx_server_packets_sent_total",
			Help: "Total packets sent by UDP server",
		},
		[]string{"addr"},
	)
)

func init() {
	prometheus.MustRegister(ServerPacketsReceived)
	prometheus.MustRegister(ServerPacketsSent)
}

// Handler handles a UDP packet.
type Handler interface {
	Handle(ctx context.Context, packet []byte, addr net.Addr, conn *net.UDPConn) error
}

// HandlerFunc adapter
type HandlerFunc func(ctx context.Context, packet []byte, addr net.Addr, conn *net.UDPConn) error

func (f HandlerFunc) Handle(ctx context.Context, packet []byte, addr net.Addr, conn *net.UDPConn) error {
	return f(ctx, packet, addr, conn)
}

// Interceptor for UDP packets
type Interceptor func(next Handler) Handler

type Server interface {
	Start(Handler) error
	Shutdown(context.Context) error
	Use(interceptors ...Interceptor)
}

type ServerConfig struct {
	Addr          string
	ReadBuffer    int
	WriteBuffer   int
	MaxPacketSize int // Default 4096
	Workers       int // Number of workers to process packets
	Trace         bool
	Logger        *logger.Logger
	EnableLogger  bool
}

type serverEntity struct {
	config       *ServerConfig
	conn         *net.UDPConn
	stopCh       chan struct{}
	isRunning    bool
	mu           sync.Mutex
	workerPool   pool.Pool
	interceptors []Interceptor
}

func NewServer(conf *ServerConfig) Server {
	if conf == nil {
		conf = &ServerConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}
	if conf.MaxPacketSize <= 0 {
		conf.MaxPacketSize = 4096
	}
	if conf.Workers <= 0 {
		conf.Workers = 10 // Default workers
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

	udpAddr, err := net.ResolveUDPAddr("udp", s.config.Addr)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.conn, err = net.ListenUDP("udp", udpAddr)
	if err != nil {
		s.mu.Unlock()
		return err
	}

	if s.config.ReadBuffer > 0 {
		err = s.conn.SetReadBuffer(s.config.ReadBuffer)
		if err != nil {
			return err
		}
	}
	if s.config.WriteBuffer > 0 {
		err = s.conn.SetWriteBuffer(s.config.WriteBuffer)
		if err != nil {
			return err
		}
	}

	// Initialize Worker Pool
	s.workerPool, err = pool.New(s.config.Workers,
		pool.WithLogger(s.config.Logger),
		pool.WithQueueSize(s.config.Workers*100), // Keep the original buffer size ratio
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

	tracer := otel.Tracer("micro/udpx")

	// Reader Loop
	bufPool := sync.Pool{
		New: func() interface{} {
			return make([]byte, s.config.MaxPacketSize)
		},
	}

	for {
		select {
		case <-stopCh:
			return nil
		default:
			// Read packet
			buf := bufPool.Get().([]byte)
			n, remoteAddr, err := s.conn.ReadFromUDP(buf)
			if err != nil {
				// If closed
				select {
				case <-stopCh:
					return nil
				default:
					s.config.Logger.Error(context.Background(), "udpx_read_error", "error", err)
					continue
				}
			}

			// Copy data to avoid race condition if buffer is reused too quickly
			payload := make([]byte, n)
			copy(payload, buf[:n])
			bufPool.Put(buf) // Return original buffer immediately

			pData := packetData{
				data: payload,
				addr: remoteAddr,
			}

			// Dispatch to worker pool
			err = s.workerPool.Submit(func() {
				s.handlePacket(pData, finalHandler, tracer)
			})
			if err != nil {
				// Pool full or closed
				s.config.Logger.Error(context.Background(), "udpx_submit_error", "error", err)
			}
		}
	}
}

type packetData struct {
	data []byte
	addr *net.UDPAddr
}

func (s *serverEntity) handlePacket(p packetData, handler Handler, tracer trace.Tracer) {
	// Metrics
	ServerPacketsReceived.WithLabelValues(s.config.Addr).Inc()

	ctx := context.Background()
	if s.config.Trace {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "udp.Handle",
			trace.WithAttributes(
				attribute.String("net.peer.ip", p.addr.String()),
				attribute.String("net.transport", "udp"),
			),
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()
	}

	start := time.Now()

	err := handler.Handle(ctx, p.data, p.addr, s.conn)
	duration := time.Since(start)

	if s.config.EnableLogger {
		if err != nil {
			s.config.Logger.Error(ctx, "udpx_handle_error", "remote", p.addr.String(), "error", err, "cost", duration.Seconds())
		} else {
			// Debug level for UDP access logs usually, as volume is high
			s.config.Logger.Debug(ctx, "udpx_handle_success", "remote", p.addr.String(), "cost", duration.Seconds())
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
	if s.conn != nil {
		s.conn.Close()
	}

	// Release worker pool
	if s.workerPool != nil {
		go s.workerPool.Release() // Async release to avoid blocking lock?
		// No, Release waits. We should probably wait outside lock?
		// Yes.
	}
	s.mu.Unlock()

	// Wait for workers via pool release (which waits for wg)
	// But we need to support context cancellation.
	// pool.Release() is blocking and doesn't take context.
	// We can wrap it.

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
