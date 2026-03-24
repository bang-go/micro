package httpx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	defaultServerAddr              = ":8080"
	defaultServerReadTimeout       = 10 * time.Second
	defaultServerReadHeaderTimeout = 5 * time.Second
	defaultServerWriteTimeout      = 10 * time.Second
	defaultServerIdleTimeout       = 30 * time.Second
	defaultServerShutdownTimeout   = 10 * time.Second
	defaultServerHealthPath        = "/healthz"
)

type Server interface {
	Start(context.Context, http.Handler) error
	Serve(context.Context, net.Listener, http.Handler) error
	Shutdown(context.Context) error
	Close() error
	HTTPServer() *http.Server
}

type serverEntity struct {
	config    *ServerConfig
	server    *http.Server
	skipPaths map[string]struct{}
	mu        sync.RWMutex
	running   bool
	metrics   *metrics
}

func NewServer(conf *ServerConfig) Server {
	if conf == nil {
		conf = &ServerConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
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

	var metrics *metrics
	if !conf.DisableMetrics {
		metrics = defaultHTTPMetrics()
		if conf.MetricsRegisterer != nil {
			metrics = newHTTPMetrics(conf.MetricsRegisterer)
		}
	}

	return &serverEntity{
		config:    conf,
		skipPaths: newSkipPathSet(defaultObservabilitySkipPaths(conf), conf.ObservabilitySkipPaths),
		metrics:   metrics,
	}
}

func (s *serverEntity) Start(ctx context.Context, handler http.Handler) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if handler == nil {
		return ErrNilHandler
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if s.config.Listener != nil {
		return s.Serve(ctx, s.config.Listener, handler)
	}
	if s.config.Addr == "" {
		return ErrServerAddrRequired
	}

	listener, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("httpx: listen: %w", err)
	}
	defer func() {
		if err != nil {
			_ = listener.Close()
		}
	}()
	err = s.Serve(ctx, listener, handler)
	return err
}

func (s *serverEntity) Serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if listener == nil {
		return ErrNilListener
	}
	if handler == nil {
		return ErrNilHandler
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrServerAlreadyRunning
	}

	server := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           s.wrapHandler(handler),
		ReadTimeout:       s.config.ReadTimeout,
		ReadHeaderTimeout: s.config.ReadHeaderTimeout,
		WriteTimeout:      s.config.WriteTimeout,
		IdleTimeout:       s.config.IdleTimeout,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	s.server = server
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.server = nil
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

	s.mu.RLock()
	server := s.server
	s.mu.RUnlock()
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
	server := s.server
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

func (s *serverEntity) HTTPServer() *http.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.server
}

func (s *serverEntity) wrapHandler(handler http.Handler) http.Handler {
	base := s.withHealthEndpoint(handler)
	base = s.recoveryMiddleware(base)

	if s.config.Trace {
		base = otelhttp.NewHandler(base, "httpx.server", otelhttp.WithFilter(func(r *http.Request) bool {
			return !matchesPath(s.skipPaths, r.URL.Path)
		}))
	}

	return s.instrumentationMiddleware(base)
}

func (s *serverEntity) withHealthEndpoint(next http.Handler) http.Handler {
	if s.config.DisableHealthEndpoint {
		return next
	}

	healthHandler := s.config.HealthHandler
	if healthHandler == nil {
		healthHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ContentTypeTextPlain)
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

func (s *serverEntity) instrumentationMiddleware(next http.Handler) http.Handler {
	if s.metrics == nil && (!s.config.EnableLogger || s.config.Logger == nil) {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if matchesPath(s.skipPaths, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		recorder := newResponseRecorder(w)
		start := time.Now()
		next.ServeHTTP(recorder, r)
		duration := time.Since(start)
		code := recorder.StatusCode()
		status := statusLabel(code)

		if s.metrics != nil {
			s.metrics.serverRequestDuration.WithLabelValues(r.Method, status).Observe(duration.Seconds())
			s.metrics.serverRequestsTotal.WithLabelValues(r.Method, status).Inc()
		}

		if s.config.EnableLogger && s.config.Logger != nil {
			fields := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", code,
				"bytes", recorder.BytesWritten(),
				"remote_addr", remoteAddrHost(r.RemoteAddr),
				"duration", duration.Seconds(),
			}
			if code >= http.StatusInternalServerError {
				s.config.Logger.Error(r.Context(), "http_server_request", fields...)
				return
			}
			if code >= http.StatusBadRequest {
				s.config.Logger.Warn(r.Context(), "http_server_request", fields...)
				return
			}
			s.config.Logger.Info(r.Context(), "http_server_request", fields...)
		}
	})
}

func (s *serverEntity) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recoveryWriter := newResponseRecorder(w)
		defer func() {
			if recovered := recover(); recovered != nil {
				s.config.Logger.Error(r.Context(), "http_server_panic_recovered",
					"error", recovered,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)

				if recoveryWriter.Written() {
					return
				}

				http.Error(recoveryWriter, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(recoveryWriter, r)
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

type responseRecorder struct {
	http.ResponseWriter
	statusCode   int
	wroteHeader  bool
	bytesWritten int64
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

func (rw *responseRecorder) Header() http.Header {
	return rw.ResponseWriter.Header()
}

func (rw *responseRecorder) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.statusCode = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseRecorder) Write(p []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(p)
	rw.bytesWritten += int64(n)
	return n, err
}

func (rw *responseRecorder) ReadFrom(src io.Reader) (int64, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	readerFrom, ok := rw.ResponseWriter.(io.ReaderFrom)
	if !ok {
		n, err := io.Copy(rw.ResponseWriter, src)
		rw.bytesWritten += n
		return n, err
	}
	n, err := readerFrom.ReadFrom(src)
	rw.bytesWritten += n
	return n, err
}

func (rw *responseRecorder) Flush() {
	flusher, ok := rw.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	flusher.Flush()
}

func (rw *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("httpx: response writer does not implement http.Hijacker")
	}
	return hijacker.Hijack()
}

func (rw *responseRecorder) Push(target string, opts *http.PushOptions) error {
	pusher, ok := rw.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (rw *responseRecorder) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func (rw *responseRecorder) StatusCode() int {
	return rw.statusCode
}

func (rw *responseRecorder) BytesWritten() int64 {
	return rw.bytesWritten
}

func (rw *responseRecorder) Written() bool {
	return rw.wroteHeader
}
