package grpcx

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	ClientRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grpc_client_request_duration_seconds",
			Help:    "gRPC client request duration in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"method", "code"},
	)

	ClientRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_client_requests_total",
			Help: "gRPC client requests total",
		},
		[]string{"method", "code"},
	)
)

type Metrics struct {
	RequestDuration *prometheus.HistogramVec
	RequestsTotal   *prometheus.CounterVec
}

var (
	defaultMetricsOnce sync.Once
	defaultMetrics     *Metrics
)

func DefaultMetrics() *Metrics {
	defaultMetricsOnce.Do(func() {
		defaultMetrics = &Metrics{
			RequestDuration: ClientRequestDuration,
			RequestsTotal:   ClientRequestsTotal,
		}
		mustRegisterCollector(prometheus.DefaultRegisterer, &defaultMetrics.RequestDuration, defaultMetrics.RequestDuration)
		mustRegisterCollector(prometheus.DefaultRegisterer, &defaultMetrics.RequestsTotal, defaultMetrics.RequestsTotal)
		ClientRequestDuration = defaultMetrics.RequestDuration
		ClientRequestsTotal = defaultMetrics.RequestsTotal
	})

	return defaultMetrics
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	metrics := &Metrics{
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "grpc_client_request_duration_seconds",
				Help:    "gRPC client request duration in seconds",
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "code"},
		),
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "grpc_client_requests_total",
				Help: "gRPC client requests total",
			},
			[]string{"method", "code"},
		),
	}
	mustRegisterCollector(registerer, &metrics.RequestDuration, metrics.RequestDuration)
	mustRegisterCollector(registerer, &metrics.RequestsTotal, metrics.RequestsTotal)
	return metrics
}

func UnaryClientMetricInterceptor() grpc.UnaryClientInterceptor {
	return UnaryClientMetricInterceptorWithMetrics(DefaultMetrics())
}

func UnaryClientMetricInterceptorWithMetrics(metrics *Metrics) grpc.UnaryClientInterceptor {
	if metrics == nil {
		metrics = DefaultMetrics()
	}

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		duration := time.Since(start).Seconds()

		code := streamStatusCode(err).String()
		metrics.RequestDuration.WithLabelValues(method, code).Observe(duration)
		metrics.RequestsTotal.WithLabelValues(method, code).Inc()

		return err
	}
}

func StreamClientMetricInterceptor() grpc.StreamClientInterceptor {
	return StreamClientMetricInterceptorWithMetrics(DefaultMetrics())
}

func StreamClientMetricInterceptorWithMetrics(metrics *Metrics) grpc.StreamClientInterceptor {
	if metrics == nil {
		metrics = DefaultMetrics()
	}

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		start := time.Now()
		clientStream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			recordClientMetrics(metrics, method, time.Since(start), err)
			return nil, err
		}

		return newObservedClientStream(clientStream, func(streamErr error) {
			recordClientMetrics(metrics, method, time.Since(start), streamErr)
		}), nil
	}
}

func recordClientMetrics(metrics *Metrics, method string, duration time.Duration, err error) {
	code := streamStatusCode(err).String()
	metrics.RequestDuration.WithLabelValues(method, code).Observe(duration.Seconds())
	metrics.RequestsTotal.WithLabelValues(method, code).Inc()
}

type observedClientStream struct {
	grpc.ClientStream
	onFinish func(error)
	once     sync.Once
}

func newObservedClientStream(stream grpc.ClientStream, onFinish func(error)) grpc.ClientStream {
	observed := &observedClientStream{
		ClientStream: stream,
		onFinish:     onFinish,
	}
	go func() {
		<-stream.Context().Done()
		observed.finish(stream.Context().Err())
	}()
	return observed
}

func (s *observedClientStream) Header() (md metadata.MD, err error) {
	md, err = s.ClientStream.Header()
	if err != nil {
		s.finish(err)
	}
	return md, err
}

func (s *observedClientStream) CloseSend() error {
	err := s.ClientStream.CloseSend()
	if err != nil {
		s.finish(err)
	}
	return err
}

func (s *observedClientStream) SendMsg(m any) error {
	err := s.ClientStream.SendMsg(m)
	if err != nil {
		s.finish(err)
	}
	return err
}

func (s *observedClientStream) RecvMsg(m any) error {
	err := s.ClientStream.RecvMsg(m)
	if err != nil {
		if errors.Is(err, io.EOF) {
			s.finish(nil)
		} else {
			s.finish(err)
		}
	}
	return err
}

func (s *observedClientStream) finish(err error) {
	s.once.Do(func() {
		if s.onFinish != nil {
			s.onFinish(err)
		}
	})
}

func streamStatusCode(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	if code := status.FromContextError(err).Code(); code != codes.Unknown {
		return code
	}
	return status.Code(err)
}
