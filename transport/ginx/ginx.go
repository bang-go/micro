package ginx

import (
	"context"
	"net/http"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	middleware "github.com/bang-go/micro/transport/ginx/middleware"
	"github.com/bang-go/util"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

type Server interface {
	Start() error
	Use(...gin.HandlerFunc)
	Engine() *http.Server
	GinEngine() *gin.Engine
	Group(relativePath string, handlers ...gin.HandlerFunc) *gin.RouterGroup
	Shutdown() error
}

type ServerConfig struct {
	ServiceName string // Service Name for Trace
	Addr        string
	Mode        string
	Trace       bool
	// TraceFilter gintrace.Filter
	Logger       *logger.Logger // Custom logger (micro/logger)
	EnableLogger bool           // Enable/Disable access logging

	// Timeouts
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type ServerEntity struct {
	*ServerConfig
	ginEngine  *gin.Engine
	httpServer *http.Server
}

func New(conf *ServerConfig) Server {
	if conf == nil {
		conf = &ServerConfig{}
	}
	mode := util.If(conf.Mode != "", conf.Mode, gin.ReleaseMode)
	gin.SetMode(mode)

	// Set default timeouts if not provided
	if conf.ReadTimeout == 0 {
		conf.ReadTimeout = 10 * time.Second
	}
	if conf.WriteTimeout == 0 {
		conf.WriteTimeout = 10 * time.Second
	}
	if conf.IdleTimeout == 0 {
		conf.IdleTimeout = 30 * time.Second
	}

	// Init logger if nil
	if conf.Logger == nil {
		if mode == gin.DebugMode {
			conf.Logger = logger.New(logger.WithLevel("debug"))
		} else {
			conf.Logger = logger.New(logger.WithLevel("info"))
		}
	}

	ginEngine := gin.New()

	// 0. Trace (OpenTelemetry) - Must be first to start span
	if conf.Trace {
		ginEngine.Use(otelgin.Middleware(util.If(conf.ServiceName != "", conf.ServiceName, "unknown-service")))
	}
	// 1. Recovery with logger
	ginEngine.Use(middleware.RecoveryMiddleware(conf.Logger, true))
	// 2. Metrics (Prometheus)
	ginEngine.Use(middleware.MetricMiddleware())
	// 3. Access Logger
	ginEngine.Use(middleware.LoggerMiddleware(conf.Logger))

	return &ServerEntity{
		ServerConfig: conf,
		ginEngine:    ginEngine,
	}
}

func (s *ServerEntity) GinEngine() *gin.Engine {
	return s.ginEngine
}

func (s *ServerEntity) Engine() *http.Server {
	return s.httpServer
}

func (s *ServerEntity) Use(middlewares ...gin.HandlerFunc) {
	s.ginEngine.Use(middlewares...)
}

func (s *ServerEntity) Start() (err error) {
	s.httpServer = &http.Server{
		Addr:    s.Addr,
		Handler: s.ginEngine,
		// Add default timeouts for production readiness
		ReadTimeout:  s.ReadTimeout,
		WriteTimeout: s.WriteTimeout,
		IdleTimeout:  s.IdleTimeout,
	}
	err = s.httpServer.ListenAndServe()
	return
}

func (s *ServerEntity) Group(relativePath string, handlers ...gin.HandlerFunc) *gin.RouterGroup {
	return s.ginEngine.Group(relativePath, handlers...)
}

func (s *ServerEntity) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}
