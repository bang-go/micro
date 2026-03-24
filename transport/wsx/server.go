package wsx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/opt"
	"github.com/coder/websocket"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Server interface {
	Start(context.Context, func(context.Context, Connect)) error
	Shutdown(context.Context) error
	Handler(func(context.Context, Connect)) http.HandlerFunc
}

type ServerConfig struct {
	Addr         string
	Listener     net.Listener
	Logger       *logger.Logger
	EnableLogger bool
	Trace        bool
	// ReadHeaderTimeout limits the time allowed to read request headers during upgrade.
	ReadHeaderTimeout time.Duration
	// IdleTimeout limits how long idle keep-alive HTTP connections are kept open.
	IdleTimeout time.Duration
	// ShutdownTimeout is used when Shutdown is called with a context that has no deadline.
	ShutdownTimeout time.Duration
	// ObservabilitySkipPaths 跳过可观测性记录（Metrics & Trace）的路径列表
	// 默认为 /healthz, /metrics。用户配置将与默认值合并。
	ObservabilitySkipPaths []string
}

type serverEntity struct {
	config      *ServerConfig
	options     *serverOptions
	server      *http.Server
	listener    net.Listener
	mu          sync.Mutex
	running     bool
	connections map[Connect]struct{}
}

func NewServer(conf *ServerConfig, opts ...opt.Option[serverOptions]) Server {
	if conf == nil {
		conf = &ServerConfig{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}
	if conf.ReadHeaderTimeout == 0 {
		conf.ReadHeaderTimeout = 5 * time.Second
	}
	if conf.IdleTimeout == 0 {
		conf.IdleTimeout = 30 * time.Second
	}
	if conf.ShutdownTimeout == 0 {
		conf.ShutdownTimeout = 10 * time.Second
	}

	options := &serverOptions{
		path: "/ws",
	}
	opt.Each(options, opts...)
	if options.path == "" {
		options.path = "/ws"
	}
	if options.checkOrigin == nil {
		options.checkOrigin = defaultCheckOrigin
	}

	s := &serverEntity{
		config:      conf,
		options:     options,
		connections: make(map[Connect]struct{}),
	}
	return s
}

func (s *serverEntity) Start(ctx context.Context, handler func(context.Context, Connect)) error {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if handler == nil {
		return errServerHandlerMissing
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errServerAlreadyRunning
	}

	var (
		lis net.Listener
		err error
	)
	if s.config.Listener != nil {
		lis = s.config.Listener
	} else {
		if s.config.Addr == "" {
			s.mu.Unlock()
			return errServerAddrMissing
		}
		lis, err = net.Listen("tcp", s.config.Addr)
		if err != nil {
			s.mu.Unlock()
			return err
		}
	}

	s.listener = lis
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.server = nil
		s.listener = nil
		s.mu.Unlock()
	}()

	mux := http.NewServeMux()
	// WebSocket Route
	mux.HandleFunc(s.options.path, s.Handler(handler))
	// Health Check Route
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	finalHandler := http.Handler(mux)
	if s.config.Trace {
		finalHandler = otelhttp.NewHandler(mux, "wsx",
			otelhttp.WithFilter(func(r *http.Request) bool {
				return !s.shouldSkipObservability(r.URL.Path)
			}),
		)
	}

	server := &http.Server{
		Addr:              lis.Addr().String(),
		Handler:           finalHandler,
		ReadHeaderTimeout: s.config.ReadHeaderTimeout,
		IdleTimeout:       s.config.IdleTimeout,
	}
	s.mu.Lock()
	s.server = server
	s.mu.Unlock()

	serveDone := make(chan struct{})
	defer close(serveDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Shutdown(context.Background())
		case <-serveDone:
		}
	}()

	s.info(ctx, "ws server starting", "addr", lis.Addr().String())

	err = server.Serve(lis)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *serverEntity) Shutdown(ctx context.Context) error {
	ctx = normalizeContext(ctx)
	if s.config.ShutdownTimeout > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, s.config.ShutdownTimeout)
			defer cancel()
		}
	}

	s.mu.Lock()
	server := s.server
	conns := s.snapshotConnectionsLocked()
	s.mu.Unlock()

	if s.options.hub != nil {
		s.options.hub.Close()
	}
	for _, conn := range conns {
		_ = conn.Close()
	}

	if server == nil {
		return nil
	}

	s.info(ctx, "ws server shutting down")

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Shutdown(ctx)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		closeErr := server.Close()
		if errors.Is(closeErr, http.ErrServerClosed) {
			closeErr = nil
		}
		return errors.Join(ctx.Err(), closeErr)
	}
}

func (s *serverEntity) info(ctx context.Context, msg string, args ...any) {
	if s.config.EnableLogger {
		s.config.Logger.Info(normalizeContext(ctx), msg, args...)
	}
}

func (s *serverEntity) Handler(handler func(context.Context, Connect)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			rawConn *websocket.Conn
			conn    Connect
		)

		// 0. Recovery
		defer func() {
			if recovered := recover(); recovered != nil {
				s.config.Logger.Error(r.Context(), "[ws_panic_recovery]",
					"error", recovered,
					"stack", string(debug.Stack()),
					"path", r.URL.Path,
				)

				if conn != nil {
					_ = conn.Close()
					return
				}
				if rawConn != nil {
					_ = rawConn.Close(websocket.StatusInternalError, "internal server error")
					return
				}

				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()

		// 1. Auth Hook
		if s.options.beforeUpgrade != nil {
			if err := s.options.beforeUpgrade(r); err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
		}

		userID := ""
		if s.options.identify != nil {
			identifiedUserID, err := s.options.identify(r.Context(), r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			userID = identifiedUserID
		}

		if s.options.checkOrigin != nil {
			if !s.options.checkOrigin(r) {
				http.Error(w, "Origin not allowed", http.StatusForbidden)
				return
			}
		}

		// Origin verification is handled above so callers can provide arbitrary policies.
		rawConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}

		s.info(r.Context(), "ws_access_log",
			"method", r.Method,
			"path", r.URL.Path,
			"ip", r.RemoteAddr,
		)

		// 2. Post-Handshake / OnConnect Hook

		connOpts := append([]opt.Option[connectOptions](nil), s.options.connectOpts...)
		if userID != "" {
			connOpts = append(connOpts, WithConnectUserID(userID))
		}
		if s.shouldSkipObservability(r.URL.Path) {
			connOpts = append(connOpts, withSkipObservability(true))
		}

		conn = NewConnect(rawConn, r.RemoteAddr, connOpts...)
		s.trackConn(conn)
		defer s.untrackConn(conn)

		if s.options.onConnect != nil {
			if err := s.options.onConnect(r.Context(), conn, r); err != nil {
				_ = conn.Close()
				return
			}
		}

		// 3. Register to Hub if present
		if s.options.hub != nil {
			if err := s.options.hub.Register(conn); err != nil {
				_ = conn.Close()
				return
			}
			defer s.options.hub.Unregister(conn)
		}

		// Ensure connection is closed when handler returns or panics
		defer conn.Close()

		handler(r.Context(), conn)
	}
}

func (s *serverEntity) shouldSkipObservability(path string) bool {
	if path == "/healthz" || path == "/metrics" {
		return true
	}
	for _, candidate := range s.config.ObservabilitySkipPaths {
		if candidate == path {
			return true
		}
	}
	return false
}

func (s *serverEntity) trackConn(conn Connect) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connections[conn] = struct{}{}
}

func (s *serverEntity) untrackConn(conn Connect) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.connections, conn)
}

func (s *serverEntity) snapshotConnectionsLocked() []Connect {
	conns := make([]Connect, 0, len(s.connections))
	for conn := range s.connections {
		conns = append(conns, conn)
	}
	return conns
}

func defaultCheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return false
	}

	requestScheme := "http"
	if r.TLS != nil {
		requestScheme = "https"
	}

	return strings.EqualFold(u.Scheme, requestScheme) &&
		normalizeHostPort(u.Host, u.Scheme) == normalizeHostPort(r.Host, requestScheme)
}

func normalizeHostPort(hostport string, scheme string) string {
	hostURL := &url.URL{Scheme: scheme, Host: hostport}
	host := strings.ToLower(hostURL.Hostname())
	port := hostURL.Port()
	if port == "" {
		switch strings.ToLower(scheme) {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	return net.JoinHostPort(host, port)
}
