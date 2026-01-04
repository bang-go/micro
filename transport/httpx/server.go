package httpx

import (
	"context"
	"net/http"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/util"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Server interface {
	Start(http.Handler) error
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
	if conf.Logger == nil && conf.EnableLogger {
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

func (s *serverEntity) Start(handler http.Handler) error {
	// 1. Wrap handler with Tracing if enabled
	var finalHandler http.Handler = handler
	if s.config.Trace {
		finalHandler = otelhttp.NewHandler(handler, "httpx-server",
			otelhttp.WithFilter(func(r *http.Request) bool {
				// Filter out health check and metrics
				if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
					return false
				}
				return true
			}),
		)
	}

	// 2. Wrap with Access Logger if enabled
	// Note: Since we are using standard net/http, implementing a middleware chain
	// similar to Gin is manual. Here we apply it around the handler.
	if s.config.EnableLogger {
		finalHandler = s.accessLoggerMiddleware(finalHandler)
	}

	// 3. Default Health Check (if not provided by handler)
	// For standard http.Server, if the user provides a ServeMux, they can add it.
	// But here we might want to ensure it exists if the user handler is a Mux.
	// A safe way is to wrap it if it's a Mux, or just rely on the user to add it.
	// However, to align with enterprise standards, we can check if it's a ServeMux
	// and register it, OR we create our own Mux that wraps the user handler?
	// The most flexible way: Use a top-level Mux.
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

	if s.config.EnableLogger {
		s.config.Logger.Info(context.Background(), "http server starting", "addr", s.server.Addr)
	}

	return s.server.ListenAndServe()
}

func (s *serverEntity) Shutdown(ctx context.Context) error {
	if s.server != nil {
		if s.config.EnableLogger {
			s.config.Logger.Info(ctx, "http server shutting down")
		}
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *serverEntity) Server() *http.Server {
	return s.server
}

// Simple Access Logger Middleware for net/http
func (s *serverEntity) accessLoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap ResponseWriter to capture status code
		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(ww, r)

		duration := time.Since(start).Seconds()
		s.config.Logger.Info(r.Context(), "http_server_access_log",
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
