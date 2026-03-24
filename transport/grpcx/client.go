package grpcx

import (
	"context"
	"crypto/tls"
	"errors"
	"runtime/debug"
	"sync"

	"github.com/bang-go/micro/telemetry/logger"
	client_interceptor "github.com/bang-go/micro/transport/grpcx/client_interceptor"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

type Client interface {
	AddDialOptions(...grpc.DialOption)
	AddUnaryInterceptor(interceptor ...grpc.UnaryClientInterceptor)
	AddStreamInterceptor(interceptor ...grpc.StreamClientInterceptor)
	Dial() (*grpc.ClientConn, error)
	DialContext(context.Context) (*grpc.ClientConn, error)
	DialWithCall(ClientCallFunc) (any, error)
	Conn() *grpc.ClientConn
	Close()
}

type ClientConfig struct {
	Addr                 string
	Secure               bool
	TLSConfig            *tls.Config
	TransportCredentials credentials.TransportCredentials
	KeepaliveParams      *keepalive.ClientParameters
	MetricsRegisterer    prometheus.Registerer
	DisableMetrics       bool
	Trace                bool
	Logger               *logger.Logger
	EnableLogger         bool
	//TraceFilter grpctrace.Filter
}

type ClientCallFunc func(*grpc.ClientConn) (any, error)

type ClientEntity struct {
	*ClientConfig
	conn               *grpc.ClientConn
	dialOptions        []grpc.DialOption
	streamInterceptors []grpc.StreamClientInterceptor
	unaryInterceptors  []grpc.UnaryClientInterceptor
	mu                 sync.RWMutex
}

func NewClient(conf *ClientConfig) Client {
	if conf == nil {
		conf = &ClientConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}

	var metrics *client_interceptor.Metrics
	if !conf.DisableMetrics {
		metrics = client_interceptor.DefaultMetrics()
		if conf.MetricsRegisterer != nil {
			metrics = client_interceptor.NewMetrics(conf.MetricsRegisterer)
		}
	}

	// Default Interceptors for Enterprise Production
	unaryInterceptors := []grpc.UnaryClientInterceptor{
		// 1. Recovery
		client_interceptor.UnaryClientRecoveryInterceptor(func(ctx context.Context, p any) error {
			conf.Logger.Error(ctx, "[Recovery from panic]", "error", p, "stack", string(debug.Stack()))
			return status.Error(codes.Internal, "grpcx: recovered from panic")
		}),
	}
	if !conf.DisableMetrics {
		unaryInterceptors = append(unaryInterceptors, client_interceptor.UnaryClientMetricInterceptorWithMetrics(metrics))
	}
	// 3. Access Logger
	if conf.EnableLogger {
		unaryInterceptors = append(unaryInterceptors, client_interceptor.UnaryClientLoggerInterceptor(conf.Logger))
	}

	streamInterceptors := []grpc.StreamClientInterceptor{
		// 1. Recovery
		client_interceptor.StreamClientRecoveryInterceptor(func(ctx context.Context, p any) error {
			conf.Logger.Error(ctx, "[Recovery from panic]", "error", p, "stack", string(debug.Stack()))
			return status.Error(codes.Internal, "grpcx: recovered from panic")
		}),
	}
	if !conf.DisableMetrics {
		streamInterceptors = append(streamInterceptors, client_interceptor.StreamClientMetricInterceptorWithMetrics(metrics))
	}
	// 3. Access Logger
	if conf.EnableLogger {
		streamInterceptors = append(streamInterceptors, client_interceptor.StreamClientLoggerInterceptor(conf.Logger))
	}

	return &ClientEntity{
		ClientConfig:       conf,
		dialOptions:        []grpc.DialOption{},
		streamInterceptors: streamInterceptors,
		unaryInterceptors:  unaryInterceptors,
	}
}

func (c *ClientEntity) Dial() (conn *grpc.ClientConn, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果连接已存在，直接返回（gRPC 会自动管理连接状态和重连）
	if c.conn != nil {
		return c.conn, nil
	}
	if c.ClientConfig.Addr == "" {
		return nil, errors.New("grpcx: client addr is required")
	}

	baseClientOption := make([]grpc.DialOption, 0, len(c.dialOptions)+4)
	if c.KeepaliveParams != nil {
		baseClientOption = append(baseClientOption, grpc.WithKeepaliveParams(*c.KeepaliveParams))
	}
	switch {
	case c.TransportCredentials != nil:
		baseClientOption = append(baseClientOption, grpc.WithTransportCredentials(c.TransportCredentials))
	case c.Secure:
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if c.TLSConfig != nil {
			tlsConfig = c.TLSConfig.Clone()
			if tlsConfig.MinVersion == 0 {
				tlsConfig.MinVersion = tls.VersionTLS12
			}
		}
		baseClientOption = append(baseClientOption, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	default:
		baseClientOption = append(baseClientOption, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	if c.Trace {
		// Trace StatsHandler
		baseClientOption = append(baseClientOption, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	}
	options := append(baseClientOption, c.dialOptions...)
	options = append(options, grpc.WithChainUnaryInterceptor(c.unaryInterceptors...), grpc.WithChainStreamInterceptor(c.streamInterceptors...))
	c.conn, err = grpc.NewClient(c.ClientConfig.Addr, options...)
	return c.conn, err
}

func (c *ClientEntity) DialContext(ctx context.Context) (*grpc.ClientConn, error) {
	if ctx == nil {
		return nil, errors.New("grpcx: context is required")
	}

	conn, err := c.Dial()
	if err != nil {
		return nil, err
	}

	if err := WaitForReady(ctx, conn); err != nil {
		return nil, err
	}

	return conn, nil
}

func (c *ClientEntity) DialWithCall(call ClientCallFunc) (any, error) {
	if call == nil {
		return nil, errors.New("grpcx: call func is required")
	}

	conn, err := c.Dial()
	if err != nil {
		return nil, err
	}
	return call(conn)
}

func (c *ClientEntity) Conn() *grpc.ClientConn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

func (c *ClientEntity) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func WaitForReady(ctx context.Context, conn *grpc.ClientConn) error {
	if ctx == nil {
		return errors.New("grpcx: context is required")
	}
	if conn == nil {
		return errors.New("grpcx: client connection is nil")
	}

	conn.Connect()

	for {
		state := conn.GetState()
		switch state {
		case connectivity.Ready:
			return nil
		case connectivity.Shutdown:
			return errors.New("grpcx: connection is shutdown")
		}

		if !conn.WaitForStateChange(ctx, state) {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
}

func (c *ClientEntity) AddDialOptions(dialOption ...grpc.DialOption) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dialOptions = append(c.dialOptions, dialOption...)
}

func (c *ClientEntity) AddUnaryInterceptor(interceptor ...grpc.UnaryClientInterceptor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unaryInterceptors = append(c.unaryInterceptors, interceptor...)
}

func (c *ClientEntity) AddStreamInterceptor(interceptor ...grpc.StreamClientInterceptor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streamInterceptors = append(c.streamInterceptors, interceptor...)
}
