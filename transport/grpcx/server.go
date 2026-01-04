package grpcx

import (
	"context"
	"fmt"
	"net"
	"runtime/debug"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	server_interceptor "github.com/bang-go/micro/transport/grpcx/server_interceptor"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
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
	healthServer       *health.Server
}

type ServerRegisterFunc func(*grpc.Server)
type ServerConfig struct {
	Addr         string
	Trace        bool
	Logger       *logger.Logger
	EnableLogger bool
	//TraceFilter grpctrace.Filter
}

// todo: trace，retry

func NewServer(conf *ServerConfig) Server {
	if conf == nil {
		conf = &ServerConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}

	// Default Interceptors for Enterprise Production
	unaryInterceptors := []grpc.UnaryServerInterceptor{
		// 1. Recovery
		server_interceptor.UnaryServerRecoveryInterceptor(func(ctx context.Context, p any) {
			conf.Logger.Error(ctx, "[Recovery from panic]", "error", p, "stack", string(debug.Stack()))
		}),
		// 2. Metrics
		server_interceptor.UnaryServerMetricInterceptor(),
	}
	// 3. Access Logger
	if conf.EnableLogger {
		unaryInterceptors = append(unaryInterceptors, server_interceptor.UnaryServerLoggerInterceptor(conf.Logger))
	}

	streamInterceptors := []grpc.StreamServerInterceptor{
		// 1. Recovery
		server_interceptor.StreamServerRecoveryInterceptor(func(ctx context.Context, p any) {
			conf.Logger.Error(ctx, "[Recovery from panic]", "error", p, "stack", string(debug.Stack()))
		}),
		// 2. Metrics
		server_interceptor.StreamServerMetricInterceptor(),
	}
	// 3. Access Logger
	if conf.EnableLogger {
		streamInterceptors = append(streamInterceptors, server_interceptor.StreamServerLoggerInterceptor(conf.Logger))
	}

	return &ServerEntity{
		ServerConfig:       conf,
		serverOptions:      nil,
		streamInterceptors: streamInterceptors,
		unaryInterceptors:  unaryInterceptors,
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
		return fmt.Errorf("grpcx: failed to listen: %w", err)
	}
	baseOptions := []grpc.ServerOption{grpc.KeepaliveEnforcementPolicy(defaultServerKeepaliveEnforcementPolicy), grpc.KeepaliveParams(defaultServerKeepaliveParams)}
	if s.Trace {
		// Trace StatsHandler
		baseOptions = append(baseOptions, grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithInterceptorFilter(func(info *otelgrpc.InterceptorInfo) bool {
				if info.Method == "/grpc.health.v1.Health/Check" || info.Method == "/grpc.health.v1.Health/Watch" {
					return false
				}
				return true
			}),
		)))
	}

	s.serverOptions = append(baseOptions, s.serverOptions...)
	options := append(s.serverOptions, grpc.ChainUnaryInterceptor(s.unaryInterceptors...), grpc.ChainStreamInterceptor(s.streamInterceptors...))
	s.grpcServer = grpc.NewServer(options...)

	// Health Check
	s.healthServer = health.NewServer()
	grpc_health_v1.RegisterHealthServer(s.grpcServer, s.healthServer)
	s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	register(s.grpcServer)

	//注册优雅退出 future
	err = s.grpcServer.Serve(lis)
	return
}

func (s *ServerEntity) Engine() *grpc.Server {
	return s.grpcServer
}

func (s *ServerEntity) Shutdown() error {
	if s.healthServer != nil {
		s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	}
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
		s.grpcServer = nil
	}
	return nil
}
