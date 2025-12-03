package throttle_test

import (
	"errors"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/bang-go/micro/throttle"
)

func TestErrorRatio(t *testing.T) {
	var err error
	breaker := throttle.Breaker()
	_ = breaker.Build(throttle.WithLogLevel(logging.InfoLevel), throttle.WithBreakerListener(throttle.DefaultStateChangeListener()))
	err = breaker.Rule([]*circuitbreaker.Rule{{
		Resource:                     "some-test",
		Strategy:                     circuitbreaker.ErrorRatio,
		RetryTimeoutMs:               3000,
		MinRequestAmount:             10,
		StatIntervalMs:               5000,
		StatSlidingWindowBucketCount: 10,
		Threshold:                    0.4,
	}})
	checkError(err)
	var sg = sync.WaitGroup{}
	var concurrent = 1000
	sg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			defer sg.Done()
			breaker.Guard("some-test", func() error {
				if rand.Uint64()%20 > 6 {
					return errors.New("biz error")
					// Record current invocation as error.
				}
				return nil
				//pass
			}, func() {
				//reject
			})
		}()
		// g1 blocked
		time.Sleep(time.Duration(rand.Uint64()%20) * time.Millisecond)
	}
	sg.Wait()
}

func TestRtRatio(t *testing.T) {
	var err error
	breaker := throttle.Breaker()
	_ = breaker.Build(throttle.WithLogLevel(logging.InfoLevel), throttle.WithBreakerListener(throttle.DefaultStateChangeListener()))
	err = breaker.Rule([]*circuitbreaker.Rule{{
		Resource:                     "some-test",
		Strategy:                     circuitbreaker.SlowRequestRatio,
		RetryTimeoutMs:               3000,
		MinRequestAmount:             10,
		StatIntervalMs:               5000,
		StatSlidingWindowBucketCount: 10,
		MaxAllowedRtMs:               50,
		Threshold:                    0.4,
	}})
	checkError(err)
	var sg = sync.WaitGroup{}
	var concurrent = 1000
	sg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			defer sg.Done()
			breaker.Guard("some-test", func() error {
				time.Sleep(time.Duration(rand.Uint64()%100+10) * time.Millisecond)
				return nil
				//pass
			}, func() {
				//reject
			})
		}()
		// g1 blocked
		time.Sleep(time.Duration(rand.Uint64()%20) * time.Millisecond)
	}
	sg.Wait()
}

func TestErrorCount(t *testing.T) {
	var err error
	breaker := throttle.Breaker()
	_ = breaker.Build(throttle.WithLogLevel(logging.InfoLevel), throttle.WithBreakerListener(throttle.DefaultStateChangeListener()))
	err = breaker.Rule([]*circuitbreaker.Rule{{
		Resource:                     "some-test",
		Strategy:                     circuitbreaker.ErrorCount,
		RetryTimeoutMs:               3000,
		MinRequestAmount:             10,
		StatIntervalMs:               5000,
		StatSlidingWindowBucketCount: 10,
		Threshold:                    50,
	}})
	checkError(err)
	var sg = sync.WaitGroup{}
	var concurrent = 1000
	sg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			defer sg.Done()
			breaker.Guard("some-test", func() error {
				if rand.Uint64()%20 > 6 {
					return errors.New("biz error")
					// Record current invocation as error.
				}
				return nil
				//pass
			}, func() {
				//reject
			})
		}()
		// g1 blocked
		time.Sleep(time.Duration(rand.Uint64()%20) * time.Millisecond)
	}
	sg.Wait()
}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}
