package throttle

import (
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/bang-go/opt"
)

type options struct {
	logLevel logging.Level
	listener circuitbreaker.StateChangeListener //breaker有效
}

func defaultOptions() *options {
	return &options{
		logLevel: logging.WarnLevel,
		listener: nil,
	}
}

func WithLogLevel(level logging.Level) opt.Option[options] {
	return opt.OptionFunc[options](func(o *options) {
		o.logLevel = level
	})
}

func WithBreakerListener(listener circuitbreaker.StateChangeListener) opt.Option[options] {
	return opt.OptionFunc[options](func(o *options) {
		o.listener = listener
	})
}
