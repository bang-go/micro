package grpcx

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	client_interceptor "github.com/bang-go/micro/transport/grpcx/client_interceptor"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type Client interface {
	AddDialOptions(...grpc.DialOption)
	AddUnaryInterceptor(interceptor ...grpc.UnaryClientInterceptor)
	AddStreamInterceptor(interceptor ...grpc.StreamClientInterceptor)
	Dial() (*grpc.ClientConn, error)
	DialWithCall(ClientCallFunc) (any, error)
	Conn() *grpc.ClientConn
	Close()
}

var defaultClientKeepaliveParams = keepalive.ClientParameters{
	Time:                10 * time.Second, // send pings every 10 seconds if there is no activity
	Timeout:             2 * time.Second,  // wait 2 second for ping ack before considering the connection dead
	PermitWithoutStream: true,             // send pings even without active streams
}

type ClientConfig struct {
	Addr         string
	Secure       bool
	Trace        bool
	Logger       *logger.Logger
	EnableLogger bool
	//TraceFilter grpctrace.Filter
}

type ClientCallFunc func(*grpc.ClientConn) (any, error)

type ClientEntity struct {
	*ClientConfig
	conn               *grpc.ClientConn
	dialOptions        []grpc.DialOption
	streamInterceptors []grpc.StreamClientInterceptor
	unaryInterceptors  []grpc.UnaryClientInterceptor
	mu                 sync.Mutex // 保护 conn 的并发访问
}

// TODO: retry, load balance

func NewClient(conf *ClientConfig) Client {
	if conf == nil {
		conf = &ClientConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}

	// Default Interceptors for Enterprise Production
	unaryInterceptors := []grpc.UnaryClientInterceptor{
		// 1. Recovery
		client_interceptor.UnaryClientRecoveryInterceptor(func(ctx context.Context, p any) {
			conf.Logger.Error(ctx, "[Recovery from panic]", "error", p, "stack", string(debug.Stack()))
		}),
		// 2. Metrics
		client_interceptor.UnaryClientMetricInterceptor(),
	}
	// 3. Access Logger
	if conf.EnableLogger {
		unaryInterceptors = append(unaryInterceptors, client_interceptor.UnaryClientLoggerInterceptor(conf.Logger))
	}

	streamInterceptors := []grpc.StreamClientInterceptor{
		// 1. Recovery
		client_interceptor.StreamClientRecoveryInterceptor(func(ctx context.Context, p any) {
			conf.Logger.Error(ctx, "[Recovery from panic]", "error", p, "stack", string(debug.Stack()))
		}),
		// 2. Metrics
		client_interceptor.StreamClientMetricInterceptor(),
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

	// 创建新连接
	baseClientOption := []grpc.DialOption{grpc.WithKeepaliveParams(defaultClientKeepaliveParams)}
	if !c.Secure {
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

func (c *ClientEntity) DialWithCall(call ClientCallFunc) (any, error) {
	conn, err := c.Dial()
	if err != nil {
		return nil, err
	}
	return call(conn)
}

func (c *ClientEntity) Conn() *grpc.ClientConn {
	// 只读操作，不需要加锁（Go 的指针读取是原子的）
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

func (c *ClientEntity) AddDialOptions(dialOption ...grpc.DialOption) {
	c.dialOptions = append(c.dialOptions, dialOption...)
}

func (c *ClientEntity) AddUnaryInterceptor(interceptor ...grpc.UnaryClientInterceptor) {
	c.unaryInterceptors = append(c.unaryInterceptors, interceptor...)
}

func (c *ClientEntity) AddStreamInterceptor(interceptor ...grpc.StreamClientInterceptor) {
	c.streamInterceptors = append(c.streamInterceptors, interceptor...)
}
