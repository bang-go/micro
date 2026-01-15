package grpcx

import (
	"context"
	"fmt"
	"net"
	"runtime/debug"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	serverinterceptor "github.com/bang-go/micro/transport/grpcx/server_interceptor"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/stats"
)

type Server interface {
	AddServerOptions(serverOption ...grpc.ServerOption)
	AddUnaryInterceptor(interceptor ...grpc.UnaryServerInterceptor)
	AddStreamInterceptor(interceptor ...grpc.StreamServerInterceptor)
	Start(context.Context, ServerRegisterFunc) error
	Engine() *grpc.Server
	Shutdown(context.Context) error
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

	// ObservabilitySkipMethods 跳过可观测性记录（Metrics & Trace）的方法列表
	// 默认为 /grpc.health.v1.Health/Check, /grpc.health.v1.Health/Watch。用户配置将与默认值合并。
	ObservabilitySkipMethods []string
}

func NewServer(conf *ServerConfig) Server {
	if conf == nil {
		conf = &ServerConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}

	// Prepare Skip Methods (Default + User Config)
	skipMethods := []string{"/grpc.health.v1.Health/Check", "/grpc.health.v1.Health/Watch"}
	skipMethods = append(skipMethods, conf.ObservabilitySkipMethods...)

	// Default Interceptors for Enterprise Production
	unaryInterceptors := []grpc.UnaryServerInterceptor{
		// 1. Recovery
		serverinterceptor.UnaryServerRecoveryInterceptor(func(ctx context.Context, p any) {
			conf.Logger.Error(ctx, "[Recovery from panic]", "error", p, "stack", string(debug.Stack()))
		}),
		// 2. Metrics
		serverinterceptor.UnaryServerMetricInterceptor(skipMethods...),
	}
	// 3. Access Logger
	if conf.EnableLogger {
		unaryInterceptors = append(unaryInterceptors, serverinterceptor.UnaryServerLoggerInterceptor(conf.Logger, skipMethods...))
	}

	streamInterceptors := []grpc.StreamServerInterceptor{
		// 1. Recovery
		serverinterceptor.StreamServerRecoveryInterceptor(func(ctx context.Context, p any) {
			conf.Logger.Error(ctx, "[Recovery from panic]", "error", p, "stack", string(debug.Stack()))
		}),
		// 2. Metrics
		serverinterceptor.StreamServerMetricInterceptor(skipMethods...),
	}
	// 3. Access Logger
	if conf.EnableLogger {
		streamInterceptors = append(streamInterceptors, serverinterceptor.StreamServerLoggerInterceptor(conf.Logger, skipMethods...))
	}

	return &ServerEntity{
		ServerConfig:       conf,
		serverOptions:      nil,
		streamInterceptors: streamInterceptors,
		unaryInterceptors:  unaryInterceptors,
	}
}

var defaultServerKeepaliveEnforcementPolicy = keepalive.EnforcementPolicy{
	MinTime:             10 * time.Second,
	PermitWithoutStream: true,
}

var defaultServerKeepaliveParams = keepalive.ServerParameters{
	MaxConnectionIdle:     infinity,
	MaxConnectionAge:      infinity,
	MaxConnectionAgeGrace: 30 * time.Second,
	Time:                  10 * time.Second,
	Timeout:               2 * time.Second,
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

func (s *ServerEntity) Start(ctx context.Context, register ServerRegisterFunc) (err error) {
	lis, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("grpcx: failed to listen: %w", err)
	}

	baseOptions := []grpc.ServerOption{
		grpc.KeepaliveEnforcementPolicy(defaultServerKeepaliveEnforcementPolicy),
		grpc.KeepaliveParams(defaultServerKeepaliveParams),
	}

	if s.Trace {
		// Prepare Skip Methods (Default + User Config)
		skipMethods := []string{"/grpc.health.v1.Health/Check", "/grpc.health.v1.Health/Watch"}
		skipMethods = append(skipMethods, s.ObservabilitySkipMethods...)

		// Trace StatsHandler
		baseOptions = append(baseOptions, grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithFilter(func(info *stats.RPCTagInfo) bool {
				for _, m := range skipMethods {
					if info.FullMethodName == m {
						return false
					}
				}
				return true
			}),
		)))
	}

	s.serverOptions = append(baseOptions, s.serverOptions...)
	options := append(s.serverOptions,
		grpc.ChainUnaryInterceptor(s.unaryInterceptors...),
		grpc.ChainStreamInterceptor(s.streamInterceptors...),
	)
	s.grpcServer = grpc.NewServer(options...)

	// Health Check
	s.healthServer = health.NewServer()
	grpc_health_v1.RegisterHealthServer(s.grpcServer, s.healthServer)
	s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	register(s.grpcServer)

	s.info(ctx, "grpc server starting", "addr", s.Addr)

	err = s.grpcServer.Serve(lis)
	return
}

func (s *ServerEntity) Engine() *grpc.Server {
	return s.grpcServer
}

func (s *ServerEntity) Shutdown(ctx context.Context) error {
	if s.healthServer != nil {
		s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	}
	if s.grpcServer != nil {
		s.info(ctx, "grpc server shutting down")
		s.grpcServer.GracefulStop()
		s.grpcServer = nil
	}
	return nil
}

func (s *ServerEntity) info(ctx context.Context, msg string, args ...any) {
	if s.EnableLogger {
		s.Logger.Info(ctx, msg, args...)
	}
}
