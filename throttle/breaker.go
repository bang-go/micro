package throttle

import (
	"log"

	sentinelApi "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/config"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/bang-go/opt"
)

type breaker struct{}

func Breaker() ThrottlerBreaker {
	return &breaker{}
}

// Rule 熔断器控制规则
func (b *breaker) Rule(rules []*circuitbreaker.Rule) error {
	_, err := circuitbreaker.LoadRules(rules)
	return err
}

func (b *breaker) Build(opts ...opt.Option[options]) error {
	o := defaultOptions()
	opt.Each(o, opts...)
	conf := config.NewDefaultConfig() //todo: 增加更多options
	// default, logging output to console
	conf.Sentinel.Log.Logger = logging.NewConsoleLogger()
	logging.ResetGlobalLoggerLevel(o.logLevel)
	err := sentinelApi.InitWithConfig(conf)
	if err != nil {
		return err
	}
	if o.listener != nil {
		circuitbreaker.RegisterStateChangeListeners(o.listener)
	}
	return nil
}

func (b *breaker) Guard(resource string, pass FuncWithErr, reject Func, opts ...sentinelApi.EntryOption) bool {
	e, block := sentinelApi.Entry(resource, opts...)
	if block != nil {
		// Blocked. We could get the block reason from the BlockError.
		log.Println("sentinel breaker reject", "msg", block.BlockMsg())
		reject()
		return false
	} else {
		// Passed, wrap the logic here.
		if err := pass(); err != nil {
			// Record current invocation as error.
			sentinelApi.TraceError(e, err)
		}
		e.Exit()
		return true
	}
}

type StateChangeListener struct{}

func DefaultStateChangeListener() *StateChangeListener {
	return &StateChangeListener{}
}
func (s *StateChangeListener) OnTransformToClosed(prev circuitbreaker.State, rule circuitbreaker.Rule) {
	//fmt.Printf("sentinel breaker trans to closed: %+v, From %s to Closed, time: %d\n", rule.Strategy, prev.String(), util.CurrentTimeMillis())
	log.Println("sentinel breaker trans to closed", "strategy", rule.Strategy.String(), "previously state", prev.String(), "time", util.CurrentTimeMillis())

}

func (s *StateChangeListener) OnTransformToOpen(prev circuitbreaker.State, rule circuitbreaker.Rule, snapshot interface{}) {
	//fmt.Printf("rule.strategy: %+v, From %s to Open, snapshot: %d, time: %d\n", rule.Strategy, prev.String(), snapshot, util.CurrentTimeMillis())
	log.Println("sentinel breaker trans to open", "strategy", rule.Strategy.String(), "previously state", prev.String(), "snapshot", snapshot, "time", util.CurrentTimeMillis())
}

func (s *StateChangeListener) OnTransformToHalfOpen(prev circuitbreaker.State, rule circuitbreaker.Rule) {
	//fmt.Printf("rule.strategy: %+v, From %s to Half-Open, time: %d\n", rule.Strategy, prev.String(), util.CurrentTimeMillis())
	log.Println("sentinel breaker trans to half-open", "strategy", rule.Strategy.String(), "previously state", prev.String(), "time", util.CurrentTimeMillis())
}
