package ginx

import "errors"

var (
	ErrContextRequired      = errors.New("ginx: context is required")
	ErrNilListener          = errors.New("ginx: listener is required")
	ErrServerAddrRequired   = errors.New("ginx: server addr or listener is required")
	ErrServerAlreadyRunning = errors.New("ginx: server already running")
)
