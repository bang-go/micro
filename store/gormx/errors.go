package gormx

import "errors"

var (
	ErrNilConfig         = errors.New("gormx: config is required")
	ErrContextRequired   = errors.New("gormx: context is required")
	ErrNilPlugin         = errors.New("gormx: plugin is required")
	ErrDriverRequired    = errors.New("gormx: driver or dialector is required")
	ErrDSNRequired       = errors.New("gormx: dsn is required when dialector is not provided")
	ErrUnsupportedDriver = errors.New("gormx: unsupported driver")
)
