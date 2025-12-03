package server_interceptor_test

import (
	"testing"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/logging"
	"github.com/bang-go/micro/grpcx/server_interceptor"
	"github.com/bang-go/micro/throttle"
	"google.golang.org/grpc"
)

func TestThrottleBreaker(t *testing.T) {
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
	handler := func() bool {
		return breaker.Guard("some-test", func() error {
			return nil
		}, func() {

		})
	}
	grpc.NewServer(grpc.ChainUnaryInterceptor(server_interceptor.UnaryServerThrottleInterceptor(handler)))
}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}
