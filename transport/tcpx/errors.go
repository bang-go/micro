package tcpx

import "errors"

var (
	ErrContextRequired      = errors.New("tcpx: context is required")
	ErrNilConn              = errors.New("tcpx: conn is required")
	ErrNilHandler           = errors.New("tcpx: handler is required")
	ErrNilListener          = errors.New("tcpx: listener is required")
	ErrClientAddrRequired   = errors.New("tcpx: client addr is required")
	ErrServerAddrRequired   = errors.New("tcpx: server addr or listener is required")
	ErrServerAlreadyRunning = errors.New("tcpx: server already running")

	errServerClosed          = errors.New("tcpx: server closed")
	errMaxConnectionsReached = errors.New("tcpx: max connections reached")
)
