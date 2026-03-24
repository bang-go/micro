package tcpx

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultClientDialTimeout  = 5 * time.Second
	defaultClientKeepAlive    = 30 * time.Second
	defaultServerKeepAlive    = 30 * time.Second
	defaultServerShutdownTime = 10 * time.Second
)

type Client interface {
	Dial() (Connect, error)
	DialContext(context.Context) (Connect, error)
}

type ClientConfig struct {
	Addr              string
	DialTimeout       time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	KeepAlivePeriod   time.Duration
	TLSConfig         *tls.Config
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
	if conf.KeepAlivePeriod == 0 {
		conf.KeepAlivePeriod = defaultClientKeepAlive
	}

	var metrics *metrics
	if !conf.DisableMetrics {
		metrics = defaultTCPMetrics()
		if conf.MetricsRegisterer != nil {
			metrics = newTCPMetrics(conf.MetricsRegisterer)
		}
	}

	return &clientEntity{config: conf, metrics: metrics}
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
		c.config.Logger.Info(ctx, "tcp client dialing", "addr", c.config.Addr)
	}

	var span trace.Span
	if c.config.Trace {
		ctx, span = otel.Tracer("micro/tcpx").Start(ctx, "tcpx.client.dial",
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
			c.config.Logger.Error(ctx, "tcp client dial failed", "addr", c.config.Addr, "error", err)
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
		span.SetAttributes(connectionAttributes(conn)...)
	}
	if c.config.EnableLogger {
		c.config.Logger.Info(ctx, "tcp client connected", "addr", c.config.Addr, "remote_addr", conn.RemoteAddr().String())
	}
	return conn, nil
}

func (c *clientEntity) dialContext(ctx context.Context) (net.Conn, error) {
	if c.config.ContextDialer != nil {
		rawConn, err := c.config.ContextDialer(ctx, "tcp", c.config.Addr)
		if err != nil {
			return nil, err
		}
		if c.config.TLSConfig == nil {
			return rawConn, nil
		}

		tlsConn := tls.Client(rawConn, clientTLSConfigForAddr(c.config.TLSConfig, c.config.Addr))
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return nil, err
		}
		return tlsConn, nil
	}

	dialer := c.newDialer()
	if c.config.TLSConfig != nil {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    clientTLSConfigForAddr(c.config.TLSConfig, c.config.Addr),
		}
		return tlsDialer.DialContext(ctx, "tcp", c.config.Addr)
	}
	return dialer.DialContext(ctx, "tcp", c.config.Addr)
}

func (c *clientEntity) newDialer() *net.Dialer {
	if c.config.Dialer != nil {
		cloned := *c.config.Dialer
		if cloned.Timeout == 0 {
			cloned.Timeout = c.config.DialTimeout
		}
		if cloned.KeepAlive == 0 {
			cloned.KeepAlive = c.config.KeepAlivePeriod
		}
		return &cloned
	}
	return &net.Dialer{
		Timeout:   c.config.DialTimeout,
		KeepAlive: c.config.KeepAlivePeriod,
	}
}

func clientDialAttributes(addr string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("network.transport", "tcp"),
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if addr != "" {
			attrs = append(attrs, attribute.String("server.address", addr))
		}
		return attrs
	}
	if host != "" {
		attrs = append(attrs, attribute.String("server.address", host))
	}
	if portNum, err := strconv.Atoi(port); err == nil && portNum > 0 {
		attrs = append(attrs, attribute.Int("server.port", portNum))
	}
	return attrs
}
