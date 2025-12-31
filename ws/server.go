package ws

import (
	"context"
	"net/http"

	"github.com/bang-go/opt"
	"github.com/coder/websocket"
)

type Server interface {
	Start(func(Connect)) error
	Shutdown(context.Context) error
	Handler(func(Connect)) http.HandlerFunc
}

type ServerConfig struct {
	Addr string
}

type serverEntity struct {
	config  *ServerConfig
	options *serverOptions
	server  *http.Server
}

func NewServer(conf *ServerConfig, opts ...opt.Option[serverOptions]) Server {
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

func (s *serverEntity) Start(handler func(Connect)) error {
	mux := http.NewServeMux()
	mux.HandleFunc(s.options.path, s.Handler(handler))
	s.server = &http.Server{
		Addr:    s.config.Addr,
		Handler: mux,
	}
	return s.server.ListenAndServe()
}

func (s *serverEntity) Shutdown(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *serverEntity) Handler(handler func(Connect)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		// 2. Post-Handshake / OnConnect Hook
		// Useful for binding UserID to connection immediately after upgrade
		c := NewConnect(conn, s.options.connectOpts...)

		if s.options.onConnect != nil {
			// Allow OnConnect to return error to close connection?
			// Or just set ID.
			// Let's pass Connect to it.
			if err := s.options.onConnect(c, r); err != nil {
				c.Close()
				return
			}
		}

		handler(c)
	}
}
