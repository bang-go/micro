package ginx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/micro/transport/ginx/middleware"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

const (
	defaultServerAddr              = ":8080"
	defaultServiceName             = "ginx"
	defaultServerReadTimeout       = 10 * time.Second
	defaultServerReadHeaderTimeout = 5 * time.Second
	defaultServerWriteTimeout      = 10 * time.Second
	defaultServerIdleTimeout       = 30 * time.Second
	defaultServerShutdownTimeout   = 10 * time.Second
	defaultServerHealthPath        = "/healthz"
)

type Server interface {
	Start(context.Context) error
	Serve(context.Context, net.Listener) error
	Close() error
	Use(...gin.HandlerFunc)
	HTTPServer() *http.Server
	GinEngine() *gin.Engine
	Group(relativePath string, handlers ...gin.HandlerFunc) *gin.RouterGroup
	Shutdown(context.Context) error
}

type ServerConfig struct {
	ServiceName  string
	Addr         string
	Listener     net.Listener
	Mode         string
	Trace        bool
	Logger       *logger.Logger
	EnableLogger bool

	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration

	ObservabilitySkipPaths []string
	MetricsRegisterer      prometheus.Registerer
	DisableMetrics         bool

	DisableHealthEndpoint bool
	HealthPath            string
	HealthHandler         http.Handler
}

type serverEntity struct {
	config     *ServerConfig
	ginEngine  *gin.Engine
	httpServer *http.Server
	listener   net.Listener
	mu         sync.RWMutex
	running    bool
}

func New(conf *ServerConfig) Server {
	if conf == nil {
		conf = &ServerConfig{}
	}
	mode := normalizeMode(conf.Mode)
	if conf.Mode != "" {
		gin.SetMode(mode)
	}
	if conf.ServiceName == "" {
		conf.ServiceName = defaultServiceName
	}
	if conf.Addr == "" && conf.Listener == nil {
		conf.Addr = defaultServerAddr
	}
	if conf.ReadTimeout == 0 {
		conf.ReadTimeout = defaultServerReadTimeout
	}
	if conf.ReadHeaderTimeout == 0 {
		conf.ReadHeaderTimeout = defaultServerReadHeaderTimeout
	}
	if conf.WriteTimeout == 0 {
		conf.WriteTimeout = defaultServerWriteTimeout
	}
	if conf.IdleTimeout == 0 {
		conf.IdleTimeout = defaultServerIdleTimeout
	}
	if conf.ShutdownTimeout == 0 {
		conf.ShutdownTimeout = defaultServerShutdownTimeout
	}
	if conf.HealthPath == "" {
		conf.HealthPath = defaultServerHealthPath
	}

	if conf.Logger == nil {
		if mode == gin.DebugMode {
			conf.Logger = logger.New(logger.WithLevel("debug"))
		} else {
			conf.Logger = logger.New(logger.WithLevel("info"))
		}
	}

	ginEngine := gin.New()
	observabilitySkipPaths := defaultObservabilitySkipPaths(conf)
	observabilitySkipPaths = append(observabilitySkipPaths, conf.ObservabilitySkipPaths...)
	skipPaths := newSkipPathSet(defaultObservabilitySkipPaths(conf), conf.ObservabilitySkipPaths)
	if conf.Trace {
		ginEngine.Use(otelgin.Middleware(
			conf.ServiceName,
			otelgin.WithFilter(func(r *http.Request) bool {
				return !matchesPath(skipPaths, r.URL.Path)
			}),
		))
	}
	if !conf.DisableMetrics {
		metrics := middleware.DefaultMetrics()
		if conf.MetricsRegisterer != nil {
			metrics = middleware.NewMetrics(conf.MetricsRegisterer)
		}
		ginEngine.Use(middleware.MetricMiddlewareWithMetrics(metrics, observabilitySkipPaths...))
	}
	if conf.EnableLogger {
		ginEngine.Use(middleware.LoggerMiddleware(conf.Logger, observabilitySkipPaths...))
	}
	ginEngine.Use(middleware.RecoveryMiddleware(conf.Logger))

	return &serverEntity{
		config:    conf,
		ginEngine: ginEngine,
	}
}

func (s *serverEntity) GinEngine() *gin.Engine {
	return s.ginEngine
}

func (s *serverEntity) HTTPServer() *http.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.httpServer
}

func (s *serverEntity) Use(middlewares ...gin.HandlerFunc) {
	s.ginEngine.Use(middlewares...)
}

func (s *serverEntity) Start(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.config.Listener != nil {
		return s.Serve(ctx, s.config.Listener)
	}
	if s.config.Addr == "" {
		return ErrServerAddrRequired
	}

	listener, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("ginx: listen: %w", err)
	}
	defer func() {
		if err != nil {
			_ = listener.Close()
		}
	}()
	err = s.Serve(ctx, listener)
	return err
}

func (s *serverEntity) Serve(ctx context.Context, listener net.Listener) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if listener == nil {
		return ErrNilListener
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrServerAlreadyRunning
	}

	server := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           s.wrapHandler(),
		ReadTimeout:       s.config.ReadTimeout,
		ReadHeaderTimeout: s.config.ReadHeaderTimeout,
		WriteTimeout:      s.config.WriteTimeout,
		IdleTimeout:       s.config.IdleTimeout,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	s.httpServer = server
	s.listener = listener
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.httpServer = nil
		s.listener = nil
		s.running = false
		s.mu.Unlock()
	}()

	done := make(chan struct{})
	defer close(done)
	go s.watchContext(ctx, done)

	s.info(ctx, "http server starting", "addr", listener.Addr().String())

	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *serverEntity) Group(relativePath string, handlers ...gin.HandlerFunc) *gin.RouterGroup {
	return s.ginEngine.Group(relativePath, handlers...)
}

func (s *serverEntity) Shutdown(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if s.config.ShutdownTimeout > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, s.config.ShutdownTimeout)
			defer cancel()
		}
	}

	s.mu.Lock()
	server := s.httpServer
	s.mu.Unlock()
	if server == nil {
		return nil
	}

	s.info(ctx, "http server shutting down")

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Shutdown(ctx)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		if ctx.Err() != nil {
			closeErr := s.Close()
			if errors.Is(closeErr, http.ErrServerClosed) {
				closeErr = nil
			}
			return errors.Join(ctx.Err(), closeErr)
		}
		return err
	case <-ctx.Done():
		closeErr := s.Close()
		if errors.Is(closeErr, http.ErrServerClosed) {
			closeErr = nil
		}
		return errors.Join(ctx.Err(), closeErr)
	}
}

func (s *serverEntity) Close() error {
	s.mu.RLock()
	server := s.httpServer
	s.mu.RUnlock()
	if server == nil {
		return nil
	}
	err := server.Close()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *serverEntity) wrapHandler() http.Handler {
	base := s.withHealthEndpoint(s.ginEngine)
	return base
}

func (s *serverEntity) withHealthEndpoint(next http.Handler) http.Handler {
	if s.config.DisableHealthEndpoint {
		return next
	}

	healthHandler := s.config.HealthHandler
	if healthHandler == nil {
		healthHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == s.config.HealthPath {
			healthHandler.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *serverEntity) watchContext(ctx context.Context, done <-chan struct{}) {
	select {
	case <-done:
		return
	case <-ctx.Done():
		_ = s.Shutdown(context.Background())
	}
}

func (s *serverEntity) info(ctx context.Context, msg string, args ...any) {
	if s.config.EnableLogger && s.config.Logger != nil {
		s.config.Logger.Info(ctx, msg, args...)
	}
}
