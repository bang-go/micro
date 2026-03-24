package redisx

import "errors"

var (
	ErrNilConfig       = errors.New("redisx: config is required")
	ErrContextRequired = errors.New("redisx: context is required")
	ErrAddrRequired    = errors.New("redisx: addr is required")
	ErrNilHook         = errors.New("redisx: hook is required")
)
