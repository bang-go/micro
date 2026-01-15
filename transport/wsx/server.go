package wsx

import (
	"context"
	"net/http"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/opt"
	"github.com/coder/websocket"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Server interface {
	Start(context.Context, func(Connect)) error
	Shutdown(context.Context) error
	Handler(func(Connect)) http.HandlerFunc
}

type ServerConfig struct {
	Addr         string
	Logger       *logger.Logger
	EnableLogger bool
	// ObservabilitySkipPaths 跳过可观测性记录（Metrics & Trace）的路径列表
	// 默认为 /healthz, /metrics。用户配置将与默认值合并。
	ObservabilitySkipPaths []string
}

type serverEntity struct {
	config  *ServerConfig
	options *serverOptions
	server  *http.Server
}

func NewServer(conf *ServerConfig, opts ...opt.Option[serverOptions]) Server {
	if conf == nil {
		conf = &ServerConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}

	options := &serverOptions{
		// Coder websocket defaults are usually good (no buffer size config needed typically)
		path:        "/ws",
		checkOrigin: func(r *http.Request) bool { return true },
	}
	opt.Each(options, opts...)

	s := &serverEntity{
		config:  conf,
		options: options,
	}
	return s
}

func (s *serverEntity) Start(ctx context.Context, handler func(Connect)) error {
	mux := http.NewServeMux()
	// WebSocket Route
	mux.HandleFunc(s.options.path, s.Handler(handler))
	// Health Check Route
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Prepare Skip Paths (Default + User Config)
	skipPaths := []string{"/healthz", "/metrics"}
	skipPaths = append(skipPaths, s.config.ObservabilitySkipPaths...)

	s.server = &http.Server{
		Addr: s.config.Addr,
		Handler: otelhttp.NewHandler(mux, "wsx",
			otelhttp.WithFilter(func(r *http.Request) bool {
				for _, p := range skipPaths {
					if r.URL.Path == p {
						return false
					}
				}
				return true
			}),
		),
	}
	s.info(ctx, "ws server starting", "addr", s.config.Addr)
	return s.server.ListenAndServe()
}

func (s *serverEntity) Shutdown(ctx context.Context) error {
	if s.options.hub != nil {
		s.options.hub.Close()
	}
	if s.server != nil {
		s.info(ctx, "ws server shutting down")
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *serverEntity) info(ctx context.Context, msg string, args ...any) {
	if s.config.EnableLogger {
		s.config.Logger.Info(ctx, msg, args...)
	}
}

func (s *serverEntity) Handler(handler func(Connect)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 0. Recovery
		defer func() {
			if err := recover(); err != nil {
				s.config.Logger.Error(r.Context(), "[ws_panic_recovery]",
					"error", err,
					"path", r.URL.Path,
				)
			}
		}()

		// 1. Auth Hook
		if s.options.beforeUpgrade != nil {
			if err := s.options.beforeUpgrade(r); err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
		}

		// Accept options
		acceptOpts := &websocket.AcceptOptions{
			InsecureSkipVerify: true, // Default to true if checkOrigin is generic, but let's see.
			// Coder's InsecureSkipVerify disables origin check.
		}

		// If user provided a specific origin check, we might need to wrap it?
		// Coder's AcceptOptions has OriginPatterns []string
		// It doesn't have a func(r) bool.
		// If we want to support custom logic, we can check it before calling Accept.

		if s.options.checkOrigin != nil {
			if !s.options.checkOrigin(r) {
				http.Error(w, "Origin not allowed", http.StatusForbidden)
				return
			}
		}

		conn, err := websocket.Accept(w, r, acceptOpts)
		if err != nil {
			// websocket.Accept handles error writing to writer?
			// Usually yes.
			return
		}

		s.info(r.Context(), "ws_access_log",
			"method", r.Method,
			"path", r.URL.Path,
			"ip", r.RemoteAddr,
		)

		// 2. Post-Handshake / OnConnect Hook
		// Useful for binding UserID to connection immediately after upgrade

		// Check Observability Skip
		skipObservability := false
		for _, p := range s.config.ObservabilitySkipPaths {
			if r.URL.Path == p {
				skipObservability = true
				break
			}
		}

		connOpts := s.options.connectOpts
		if skipObservability {
			connOpts = append(connOpts, WithSkipObservability(true))
		}

		c := NewConnect(conn, connOpts...)

		if s.options.onConnect != nil {
			// Allow OnConnect to return error to close connection?
			// Or just set ID.
			// Let's pass Connect to it.
			if err := s.options.onConnect(c, r); err != nil {
				c.Close()
				return
			}
		}

		// Ensure connection is closed when handler returns or panics
		defer c.Close()

		handler(c)
	}
}
