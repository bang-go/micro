package grpcx

import (
	"sync"
	"time"

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
	Addr   string
	Secure bool
	Trace  bool
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

// TODO: metric, retry, load balance

func NewClient(conf *ClientConfig) Client {
	return &ClientEntity{
		ClientConfig:       conf,
		dialOptions:        []grpc.DialOption{},
		streamInterceptors: []grpc.StreamClientInterceptor{},
		unaryInterceptors:  []grpc.UnaryClientInterceptor{},
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
	//if c.Trace {
	//	traceOption := []grpc.DialOption{grpc.WithChainUnaryInterceptor(grpctrace.UnaryClientInterceptor()), grpc.WithChainStreamInterceptor(grpctrace.StreamClientInterceptor())}
	//	if c.TraceFilter != nil {
	//		traceOption = []grpc.DialOption{grpc.WithChainUnaryInterceptor(grpctrace.UnaryClientInterceptor(grpctrace.WithFilter(c.TraceFilter))), grpc.WithChainStreamInterceptor(grpctrace.StreamClientInterceptor(grpctrace.WithFilter(c.TraceFilter)))}
	//	}
	//	baseClientOption = append(baseClientOption, traceOption...)
	//}
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
