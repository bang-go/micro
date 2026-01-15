package httpx

import (
	"context"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/util"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Server interface {
	Start(context.Context, http.Handler) error
	Shutdown(context.Context) error
	Server() *http.Server
}

type serverEntity struct {
	config *Config
	server *http.Server
}

func NewServer(conf *Config) Server {
	if conf == nil {
		conf = &Config{}
	}
	// Always ensure a logger exists to prevent panics, but only use it if EnableLogger is true
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}
	if conf.ReadTimeout == 0 {
		conf.ReadTimeout = 10 * time.Second
	}
	if conf.WriteTimeout == 0 {
		conf.WriteTimeout = 10 * time.Second
	}
	if conf.IdleTimeout == 0 {
		conf.IdleTimeout = 30 * time.Second
	}

	return &serverEntity{
		config: conf,
	}
}

func (s *serverEntity) Start(ctx context.Context, handler http.Handler) error {
	// 0. Wrap with Recovery (Must be outermost)
	var finalHandler http.Handler = s.recoveryMiddleware(handler)

	// 1. Wrap handler with Tracing if enabled
	if s.config.Trace {
		finalHandler = otelhttp.NewHandler(finalHandler, "httpx-server",
			otelhttp.WithFilter(func(r *http.Request) bool {
				// Filter out health check and metrics
				if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
					return false
				}
				return true
			}),
		)
	}

	// 2. Wrap with Access Logger
	finalHandler = s.accessLoggerMiddleware(finalHandler)

	// 3. Default Health Check
	mux := http.NewServeMux()
	mux.Handle("/", finalHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	s.server = &http.Server{
		Addr:         util.If(s.config.Addr != "", s.config.Addr, ":8080"),
		Handler:      mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	s.info(ctx, "http server starting", "addr", s.server.Addr)

	return s.server.ListenAndServe()
}

func (s *serverEntity) Shutdown(ctx context.Context) error {
	if s.server != nil {
		s.info(ctx, "http server shutting down")
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *serverEntity) Server() *http.Server {
	return s.server
}

func (s *serverEntity) info(ctx context.Context, msg string, args ...any) {
	if s.config.EnableLogger {
		s.config.Logger.Info(ctx, msg, args...)
	}
}

func (s *serverEntity) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// Always log panics, bypassing EnableLogger check
				s.config.Logger.Error(r.Context(), "[Recovery from panic]",
					"error", err,
					"stack", string(debug.Stack()),
					"path", r.URL.Path,
				)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Simple Access Logger Middleware for net/http
func (s *serverEntity) accessLoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.config.EnableLogger {
			next.ServeHTTP(w, r)
			return
		}

		// Prepare Skip Paths (Default + User Config)
		skipPaths := map[string]struct{}{
			"/healthz": {},
			"/metrics": {},
		}
		for _, p := range s.config.ObservabilitySkipPaths {
			skipPaths[p] = struct{}{}
		}

		// Skip logging if path is in skip list
		if _, ok := skipPaths[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		// Wrap ResponseWriter to capture status code
		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(ww, r)

		duration := time.Since(start).Seconds()
		s.info(r.Context(), "http_server_access_log",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.statusCode,
			"ip", r.RemoteAddr,
			"duration", duration,
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
