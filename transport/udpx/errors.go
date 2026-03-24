package udpx

import "errors"

var (
	ErrContextRequired      = errors.New("udpx: context is required")
	ErrNilConn              = errors.New("udpx: conn is required")
	ErrDatagramConnRequired = errors.New("udpx: conn must implement datagram semantics")
	ErrNilHandler           = errors.New("udpx: handler is required")
	ErrNilPacketConn        = errors.New("udpx: packet conn is required")
	ErrClientAddrRequired   = errors.New("udpx: client addr is required")
	ErrServerAddrRequired   = errors.New("udpx: server addr or packet conn is required")
	ErrServerAlreadyRunning = errors.New("udpx: server already running")

	errServerClosed = errors.New("udpx: server closed")
)
