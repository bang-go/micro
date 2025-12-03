package grpcx

import (
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/keepalive"
)

type Server interface {
	AddServerOptions(serverOption ...grpc.ServerOption)
	AddUnaryInterceptor(interceptor ...grpc.UnaryServerInterceptor)
	AddStreamInterceptor(interceptor ...grpc.StreamServerInterceptor)
	Start(ServerRegisterFunc) error
	Engine() *grpc.Server
	Shutdown() error
}

type ServerEntity struct {
	*ServerConfig
	serverOptions      []grpc.ServerOption
	streamInterceptors []grpc.StreamServerInterceptor
	unaryInterceptors  []grpc.UnaryServerInterceptor
	grpcServer         *grpc.Server
}

type ServerRegisterFunc func(*grpc.Server)
type ServerConfig struct {
	Addr  string
	Trace bool
	//TraceFilter grpctrace.Filter
}

// todo: trace，metric，retry

func NewServer(conf *ServerConfig) Server {
	return &ServerEntity{
		ServerConfig:       conf,
		serverOptions:      nil,
		streamInterceptors: nil,
		unaryInterceptors:  nil,
	}
}

var defaultServerKeepaliveEnforcementPolicy = keepalive.EnforcementPolicy{
	MinTime:             10 * time.Second, // If a client pings more than once every 5 seconds, terminate the connection
	PermitWithoutStream: true,             // Allow pings even when there are no active streams
}

var defaultServerKeepaliveParams = keepalive.ServerParameters{
	MaxConnectionIdle:     infinity,         // If a client is idle for 15 seconds, send a GOAWAY
	MaxConnectionAge:      infinity,         // If any connection is alive for more than 30 seconds, send a GOAWAY
	MaxConnectionAgeGrace: 30 * time.Second, // Allow 530seconds for pending RPCs to complete before forcibly closing connections
	Time:                  10 * time.Second, // Ping the client if it is idle for 5 seconds to ensure the connection is still active
	Timeout:               2 * time.Second,  // Wait 1 second for the ping ack before assuming the connection is dead
}

func (s *ServerEntity) AddServerOptions(serverOption ...grpc.ServerOption) {
	s.serverOptions = append(s.serverOptions, serverOption...)
}

func (s *ServerEntity) AddUnaryInterceptor(interceptor ...grpc.UnaryServerInterceptor) {
	s.unaryInterceptors = append(s.unaryInterceptors, interceptor...)
}

func (s *ServerEntity) AddStreamInterceptor(interceptor ...grpc.StreamServerInterceptor) {
	s.streamInterceptors = append(s.streamInterceptors, interceptor...)
}

func (s *ServerEntity) Start(register ServerRegisterFunc) (err error) {
	lis, err := net.Listen("tcp", s.Addr)
	if err != nil {
		grpclog.Fatalf("Failed to listen: %v", err)
	}
	baseOptions := []grpc.ServerOption{grpc.KeepaliveEnforcementPolicy(defaultServerKeepaliveEnforcementPolicy), grpc.KeepaliveParams(defaultServerKeepaliveParams)}
	//if s.Trace {
	//	traceOption := []grpc.ServerOption{grpc.ChainUnaryInterceptor(grpctrace.UnaryServerInterceptor()), grpc.ChainStreamInterceptor(grpctrace.StreamServerInterceptor())}
	//	if s.TraceFilter != nil {
	//		traceOption = []grpc.ServerOption{grpc.ChainUnaryInterceptor(grpctrace.UnaryServerInterceptor(grpctrace.WithFilter(s.TraceFilter))), grpc.ChainStreamInterceptor(grpctrace.StreamServerInterceptor(grpctrace.WithFilter(s.TraceFilter)))}
	//	}
	//	baseOptions = append(baseOptions, traceOption...)
	//}

	s.serverOptions = append(baseOptions, s.serverOptions...)
	options := append(s.serverOptions, grpc.ChainUnaryInterceptor(s.unaryInterceptors...), grpc.ChainStreamInterceptor(s.streamInterceptors...))
	s.grpcServer = grpc.NewServer(options...)
	register(s.grpcServer)

	//注册优雅退出 future
	err = s.grpcServer.Serve(lis)
	return
}

func (s *ServerEntity) Engine() *grpc.Server {
	return s.grpcServer
}

func (s *ServerEntity) Shutdown() error {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
		s.grpcServer = nil
	}
	return nil
}
