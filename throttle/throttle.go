package throttle

import (
	sentinelApi "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/bang-go/opt"
)

type Func func()
type FuncWithErr func() error

type Throttler interface {
	Build(...opt.Option[options]) error
	Guard(resource string, pass FuncWithErr, reject Func, opts ...sentinelApi.EntryOption) bool
}

type ThrottlerBreaker interface {
	Throttler
	Rule(rules []*circuitbreaker.Rule) error
}

type ThrottlerLimiter interface {
	Throttler
	Rule(rules []*flow.Rule) error
}
