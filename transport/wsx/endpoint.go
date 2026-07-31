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

	"github.com/coder/websocket"
)

const connectionCleanupTimeout = 5 * time.Second

type Session interface {
	UserID() string
	Serve(context.Context, Connect) error
}

type PrepareFunc func(context.Context, *http.Request) (Session, error)

type Endpoint struct {
	config  EndpointConfig
	hub     Hub
	prepare PrepareFunc

	mu      sync.Mutex
	started bool
	closed  bool
	rootCtx context.Context
	cancel  context.CancelFunc
	conns   map[Connect]context.CancelFunc
	wg      sync.WaitGroup
}

func NewEndpoint(config EndpointConfig, hub Hub, prepare PrepareFunc) (*Endpoint, error) {
	if hub == nil {
		return nil, ErrHubRequired
	}
	if prepare == nil {
		return nil, ErrPrepareRequired
	}
	if config.CheckOrigin == nil {
		config.CheckOrigin = defaultCheckOrigin
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 20 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 10 * time.Second
	}
	if config.Logger == nil {
		return nil, ErrEndpointLoggerRequired
	}
	return &Endpoint{
		config:  config,
		hub:     hub,
		prepare: prepare,
		conns:   make(map[Connect]context.CancelFunc),
	}, nil
}

func (e *Endpoint) Start(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrEndpointClosed
	}
	if e.started {
		return ErrEndpointAlreadyStarted
	}
	rootCtx, cancel := context.WithCancel(ctx)
	e.rootCtx = rootCtx
	e.cancel = cancel

	if err := e.hub.start(rootCtx); err != nil {
		cancel()
		e.rootCtx = nil
		e.cancel = nil
		return err
	}
	e.started = true
	return nil
}

func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	available := e.started && !e.closed && e.rootCtx != nil && e.rootCtx.Err() == nil
	rootCtx := e.rootCtx
	e.mu.Unlock()
	if !available {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	if !e.config.CheckOrigin(r) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	session, err := e.prepare(r.Context(), r)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	if session == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(session.UserID()) == "" {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	raw, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}

	connectionCtx, cancelConnection := context.WithCancel(context.WithoutCancel(r.Context()))
	stopRootCancel := context.AfterFunc(rootCtx, cancelConnection)
	conn, sessionCtx := newConnect(
		connectionCtx,
		raw,
		session.UserID(),
		r.RemoteAddr,
		e.config.HeartbeatInterval,
		e.config.WriteTimeout,
		false,
	)

	e.mu.Lock()
	if e.closed || !e.started || rootCtx.Err() != nil {
		e.mu.Unlock()
		stopRootCancel()
		cancelConnection()
		_ = conn.CloseNow()
		return
	}
	e.conns[conn] = cancelConnection
	e.wg.Add(1)
	e.mu.Unlock()

	go e.serveConnection(sessionCtx, conn, session, cancelConnection, stopRootCancel)
}

func (e *Endpoint) serveConnection(
	ctx context.Context,
	conn Connect,
	session Session,
	cancel context.CancelFunc,
	stopRootCancel func() bool,
) {
	registered := false
	defer func() {
		if recovered := recover(); recovered != nil {
			e.config.Logger.Error(ctx, "websocket_connection_panic",
				"error", recovered,
				"stack", string(debug.Stack()),
				"user_id", conn.UserID(),
				"session_id", conn.SessionID(),
			)
		}
		if registered {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), connectionCleanupTimeout)
			if err := e.hub.Unregister(cleanupCtx, conn); err != nil {
				e.config.Logger.Error(cleanupCtx, "websocket_connection_unregister_failed",
					"error", err,
					"user_id", conn.UserID(),
					"session_id", conn.SessionID(),
				)
			}
			cleanupCancel()
		}
		_ = conn.CloseNow()
		stopRootCancel()
		cancel()
		e.mu.Lock()
		delete(e.conns, conn)
		e.mu.Unlock()
		e.wg.Done()
	}()

	if err := e.hub.Register(ctx, conn); err != nil {
		e.config.Logger.Error(ctx, "websocket_connection_register_failed",
			"error", err,
			"user_id", conn.UserID(),
			"session_id", conn.SessionID(),
		)
		return
	}
	registered = true
	if err := session.Serve(ctx, conn); err != nil {
		e.config.Logger.Error(ctx, "websocket_connection_serve_failed",
			"error", err,
			"user_id", conn.UserID(),
			"session_id", conn.SessionID(),
		)
	}
}

func (e *Endpoint) Shutdown(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}

	e.mu.Lock()
	if !e.started && !e.closed {
		e.mu.Unlock()
		return ErrEndpointNotStarted
	}
	e.closed = true
	cancel := e.cancel
	conns := make([]Connect, 0, len(e.conns))
	for conn := range e.conns {
		conns = append(conns, conn)
	}
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	var closeErr error
	for _, conn := range conns {
		closeErr = errors.Join(closeErr, conn.CloseNow())
	}

	waitDone := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(waitDone)
	}()
	var waitErr error
	select {
	case <-waitDone:
	case <-ctx.Done():
		waitErr = ctx.Err()
	}

	hubErr := e.hub.shutdown(ctx)
	return errors.Join(closeErr, waitErr, hubErr)
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
		if strings.EqualFold(scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(host, port)
}
