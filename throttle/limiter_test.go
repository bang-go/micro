package throttle_test

import (
	"sync"
	"testing"

	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/bang-go/micro/throttle"
)

func TestFlow(t *testing.T) {
	var err error
	limiter := throttle.Limiter()
	_ = limiter.Build(throttle.WithLogLevel(logging.InfoLevel))
	err = limiter.Rule([]*flow.Rule{{
		Resource:               "some-test",
		TokenCalculateStrategy: flow.Direct,
		ControlBehavior:        flow.Reject,
		StatIntervalInMs:       1000,
		Threshold:              10,
	}})
	limitCheckError(err)
	var sg = sync.WaitGroup{}
	var concurrent = 100
	sg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			defer sg.Done()
			limiter.Guard("some-test", func() error {
				return nil
				//pass
			}, func() {
				//reject
			})
		}()
	}
	sg.Wait()
}

func limitCheckError(err error) {
	if err != nil {
		panic(err)
	}
}
