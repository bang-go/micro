package udpx

import (
	"context"
	"net"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultClientDialTimeout    = 5 * time.Second
	defaultServerShutdownTime   = 10 * time.Second
	defaultServerMaxPacketSize  = 64 * 1024
	defaultServerMaxConcurrency = 256
)

type Client interface {
	Dial() (Connect, error)
	DialContext(context.Context) (Connect, error)
}

type ClientConfig struct {
	Addr              string
	LocalAddr         net.Addr
	DialTimeout       time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	Dialer            *net.Dialer
	ContextDialer     func(context.Context, string, string) (net.Conn, error)
	Logger            *logger.Logger
	EnableLogger      bool
	Trace             bool
	MetricsRegisterer prometheus.Registerer
	DisableMetrics    bool
}

type clientEntity struct {
	config  *ClientConfig
	metrics *metrics
}

func NewClient(conf *ClientConfig) Client {
	if conf == nil {
		conf = &ClientConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}
	if conf.DialTimeout == 0 {
		conf.DialTimeout = defaultClientDialTimeout
	}

	var metrics *metrics
	if !conf.DisableMetrics {
		metrics = defaultUDPMetrics()
		if conf.MetricsRegisterer != nil {
			metrics = newUDPMetrics(conf.MetricsRegisterer)
		}
	}

	return &clientEntity{
		config:  conf,
		metrics: metrics,
	}
}

func (c *clientEntity) Dial() (Connect, error) {
	return c.DialContext(context.Background())
}

func (c *clientEntity) DialContext(ctx context.Context) (Connect, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.config.Addr == "" {
		return nil, ErrClientAddrRequired
	}

	start := time.Now()
	result := "error"
	defer func() {
		if c.metrics != nil {
			c.metrics.clientDialDuration.WithLabelValues(result).Observe(time.Since(start).Seconds())
			c.metrics.clientDialsTotal.WithLabelValues(result).Inc()
		}
	}()

	if c.config.EnableLogger {
		c.config.Logger.Info(ctx, "udp client dialing", "addr", c.config.Addr)
	}

	var span trace.Span
	if c.config.Trace {
		ctx, span = otel.Tracer("micro/udpx").Start(ctx, "udpx.client.dial",
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(clientDialAttributes(c.config.Addr)...),
		)
		defer span.End()
	}

	rawConn, err := c.dialContext(ctx)
	if err != nil {
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		if c.config.EnableLogger {
			c.config.Logger.Error(ctx, "udp client dial failed", "addr", c.config.Addr, "error", err)
		}
		return nil, err
	}

	conn, err := NewConnect(rawConn,
		WithReadTimeout(c.config.ReadTimeout),
		WithWriteTimeout(c.config.WriteTimeout),
	)
	if err != nil {
		if rawConn != nil {
			_ = rawConn.Close()
		}
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return nil, err
	}

	result = "success"
	if span != nil {
		span.SetAttributes(connectionAttributes(conn.LocalAddr(), conn.RemoteAddr())...)
	}
	if c.config.EnableLogger {
		c.config.Logger.Info(ctx, "udp client connected", "addr", c.config.Addr, "remote_addr", conn.RemoteAddr().String())
	}
	return conn, nil
}

func (c *clientEntity) dialContext(ctx context.Context) (net.Conn, error) {
	if c.config.ContextDialer != nil {
		return c.config.ContextDialer(ctx, "udp", c.config.Addr)
	}
	return c.newDialer().DialContext(ctx, "udp", c.config.Addr)
}

func (c *clientEntity) newDialer() *net.Dialer {
	if c.config.Dialer != nil {
		cloned := *c.config.Dialer
		if cloned.Timeout == 0 {
			cloned.Timeout = c.config.DialTimeout
		}
		if cloned.LocalAddr == nil && c.config.LocalAddr != nil {
			cloned.LocalAddr = c.config.LocalAddr
		}
		return &cloned
	}
	return &net.Dialer{
		Timeout:   c.config.DialTimeout,
		LocalAddr: c.config.LocalAddr,
	}
}

func clientDialAttributes(addr string) []attribute.KeyValue {
	host, port := splitAddr(stringAddr(addr))
	attrs := []attribute.KeyValue{
		attribute.String("network.transport", "udp"),
	}
	if host != "" {
		attrs = append(attrs, attribute.String("server.address", host))
	}
	if port > 0 {
		attrs = append(attrs, attribute.Int("server.port", port))
	}
	return attrs
}

type stringAddr string

func (s stringAddr) Network() string { return "udp" }
func (s stringAddr) String() string  { return string(s) }
