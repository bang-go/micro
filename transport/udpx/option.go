package udpx

import (
	"time"

	"github.com/bang-go/opt"
)

type connectOptions struct {
	readTimeout  time.Duration
	writeTimeout time.Duration
}

func WithReadTimeout(timeout time.Duration) opt.Option[connectOptions] {
	return opt.OptionFunc[connectOptions](func(o *connectOptions) {
		o.readTimeout = timeout
	})
}

func WithWriteTimeout(timeout time.Duration) opt.Option[connectOptions] {
	return opt.OptionFunc[connectOptions](func(o *connectOptions) {
		o.writeTimeout = timeout
	})
}
