package grpcx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime/debug"
	"sync"

	"github.com/bang-go/micro/telemetry/logger"
	serverinterceptor "github.com/bang-go/micro/transport/grpcx/server_interceptor"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
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
	listener           net.Listener
	mu                 sync.RWMutex
	running            bool
}

type ServerRegisterFunc func(*grpc.Server)
type ServerConfig struct {
	Addr                       string
	Listener                   net.Listener
	KeepaliveEnforcementPolicy *keepalive.EnforcementPolicy
	KeepaliveParams            *keepalive.ServerParameters
	MetricsRegisterer          prometheus.Registerer
	DisableMetrics             bool
	Trace                      bool
	Logger                     *logger.Logger
	EnableLogger               bool
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

	var metrics *serverinterceptor.Metrics
	if !conf.DisableMetrics {
		metrics = serverinterceptor.DefaultMetrics()
		if conf.MetricsRegisterer != nil {
			metrics = serverinterceptor.NewMetrics(conf.MetricsRegisterer)
		}
	}

	// Default Interceptors for Enterprise Production
	unaryInterceptors := []grpc.UnaryServerInterceptor{
		// 1. Recovery
		serverinterceptor.UnaryServerRecoveryInterceptor(func(ctx context.Context, p any) error {
			conf.Logger.Error(ctx, "[Recovery from panic]", "error", p, "stack", string(debug.Stack()))
			return status.Error(codes.Internal, "grpcx: recovered from panic")
		}),
	}
	if !conf.DisableMetrics {
		unaryInterceptors = append(unaryInterceptors, serverinterceptor.UnaryServerMetricInterceptorWithMetrics(metrics, skipMethods...))
	}
	// 3. Access Logger
	if conf.EnableLogger {
		unaryInterceptors = append(unaryInterceptors, serverinterceptor.UnaryServerLoggerInterceptor(conf.Logger, skipMethods...))
	}

	streamInterceptors := []grpc.StreamServerInterceptor{
		// 1. Recovery
		serverinterceptor.StreamServerRecoveryInterceptor(func(ctx context.Context, p any) error {
			conf.Logger.Error(ctx, "[Recovery from panic]", "error", p, "stack", string(debug.Stack()))
			return status.Error(codes.Internal, "grpcx: recovered from panic")
		}),
	}
	if !conf.DisableMetrics {
		streamInterceptors = append(streamInterceptors, serverinterceptor.StreamServerMetricInterceptorWithMetrics(metrics, skipMethods...))
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

func (s *ServerEntity) AddServerOptions(serverOption ...grpc.ServerOption) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serverOptions = append(s.serverOptions, serverOption...)
}

func (s *ServerEntity) AddUnaryInterceptor(interceptor ...grpc.UnaryServerInterceptor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unaryInterceptors = append(s.unaryInterceptors, interceptor...)
}

func (s *ServerEntity) AddStreamInterceptor(interceptor ...grpc.StreamServerInterceptor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamInterceptors = append(s.streamInterceptors, interceptor...)
}

func (s *ServerEntity) Start(ctx context.Context, register ServerRegisterFunc) (err error) {
	if ctx == nil {
		return errors.New("grpcx: context is required")
	}
	if register == nil {
		return errors.New("grpcx: register func is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("grpcx: server already running")
	}

	var lis net.Listener
	if s.Listener != nil {
		lis = s.Listener
	} else {
		if s.Addr == "" {
			s.mu.Unlock()
			return errors.New("grpcx: server addr or listener is required")
		}

		lis, err = net.Listen("tcp", s.Addr)
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("grpcx: failed to listen: %w", err)
		}
	}
	s.listener = lis
	s.running = true

	unaryInterceptors := append([]grpc.UnaryServerInterceptor(nil), s.unaryInterceptors...)
	streamInterceptors := append([]grpc.StreamServerInterceptor(nil), s.streamInterceptors...)
	serverOptions := append([]grpc.ServerOption(nil), s.serverOptions...)
	s.mu.Unlock()

	serveFinished := false
	defer func() {
		s.mu.Lock()
		s.running = false
		s.listener = nil
		s.grpcServer = nil
		s.healthServer = nil
		s.mu.Unlock()

		if !serveFinished && lis != nil {
			_ = lis.Close()
		}
	}()

	baseOptions := make([]grpc.ServerOption, 0, len(serverOptions)+4)
	if s.KeepaliveEnforcementPolicy != nil {
		baseOptions = append(baseOptions, grpc.KeepaliveEnforcementPolicy(*s.KeepaliveEnforcementPolicy))
	}
	if s.KeepaliveParams != nil {
		baseOptions = append(baseOptions, grpc.KeepaliveParams(*s.KeepaliveParams))
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

	serverOptions = append(baseOptions, serverOptions...)
	options := append(serverOptions,
		grpc.ChainUnaryInterceptor(unaryInterceptors...),
		grpc.ChainStreamInterceptor(streamInterceptors...),
	)
	grpcServer := grpc.NewServer(options...)
	healthServer := health.NewServer()

	// Health Check
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	s.mu.Lock()
	s.grpcServer = grpcServer
	s.healthServer = healthServer
	s.mu.Unlock()

	register(grpcServer)

	s.info(ctx, "grpc server starting", "addr", lis.Addr().String())

	serverDone := make(chan struct{})
	defer close(serverDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Shutdown(context.Background())
		case <-serverDone:
		}
	}()

	err = grpcServer.Serve(lis)
	serveFinished = true
	if errors.Is(err, grpc.ErrServerStopped) {
		return nil
	}
	return
}

func (s *ServerEntity) Engine() *grpc.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.grpcServer
}

func (s *ServerEntity) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("grpcx: context is required")
	}

	s.mu.RLock()
	healthServer := s.healthServer
	grpcServer := s.grpcServer
	s.mu.RUnlock()

	if healthServer != nil {
		healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	}
	if grpcServer == nil {
		return nil
	}

	s.info(ctx, "grpc server shutting down")
	done := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		grpcServer.Stop()
		return ctx.Err()
	}

	return nil
}

func (s *ServerEntity) info(ctx context.Context, msg string, args ...any) {
	if s.EnableLogger {
		s.Logger.Info(ctx, msg, args...)
	}
}
