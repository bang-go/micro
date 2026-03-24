package httpx

import "errors"

var (
	ErrContextRequired            = errors.New("httpx: context is required")
	ErrNilRequest                 = errors.New("httpx: request is required")
	ErrRequestMethodRequired      = errors.New("httpx: request method is required")
	ErrRequestURLRequired         = errors.New("httpx: request url is required")
	ErrRequestAbsoluteURLRequired = errors.New("httpx: request url must be absolute")
	ErrNilResponse                = errors.New("httpx: response is nil")
	ErrNilHandler                 = errors.New("httpx: handler is required")
	ErrNilListener                = errors.New("httpx: listener is required")
	ErrServerAddrRequired         = errors.New("httpx: server addr or listener is required")
	ErrServerAlreadyRunning       = errors.New("httpx: server already running")
)
