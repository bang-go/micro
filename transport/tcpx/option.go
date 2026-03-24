package tcpx

import (
	"time"

	"github.com/bang-go/opt"
)

type connectOptions struct {
	readTimeout  time.Duration
	writeTimeout time.Duration
	onRead       func(int)
	onWrite      func(int)
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

func withReadObserver(observer func(int)) opt.Option[connectOptions] {
	return opt.OptionFunc[connectOptions](func(o *connectOptions) {
		o.onRead = observer
	})
}

func withWriteObserver(observer func(int)) opt.Option[connectOptions] {
	return opt.OptionFunc[connectOptions](func(o *connectOptions) {
		o.onWrite = observer
	})
}
