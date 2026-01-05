package pool

import (
	"github.com/bang-go/micro/telemetry/logger"
)

// Option defines a functional option for the Pool.
type Option func(*options)

type options struct {
	panicHandler func(interface{})
	logger       *logger.Logger
	nonBlocking  bool
	queueSize    int
}

// WithPanicHandler sets a callback for when a worker panics.
// If set, this handler is called with the recover() result.
func WithPanicHandler(h func(interface{})) Option {
	return func(o *options) {
		o.panicHandler = h
	}
}

// WithLogger sets the logger for the pool to log panics if no PanicHandler is set.
func WithLogger(l *logger.Logger) Option {
	return func(o *options) {
		o.logger = l
	}
}

// WithNonBlocking makes Submit return immediately with ErrPoolFull if the pool (queue) is full.
// Default is false (blocking).
func WithNonBlocking(b bool) Option {
	return func(o *options) {
		o.nonBlocking = b
	}
}

// WithQueueSize sets the size of the task queue (buffered channel).
// If 0 or negative, it defaults to the pool size.
func WithQueueSize(size int) Option {
	return func(o *options) {
		o.queueSize = size
	}
}
