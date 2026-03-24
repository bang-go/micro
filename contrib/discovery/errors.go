package discovery

import "errors"

var (
	ErrNilConfig        = errors.New("discovery: config is required")
	ErrServerConfigMiss = errors.New("discovery: server configs or client endpoint is required")
)
