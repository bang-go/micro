package grpcx

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
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

func init() {
	prometheus.MustRegister(ClientRequestDuration)
	prometheus.MustRegister(ClientRequestsTotal)
}

func UnaryClientMetricInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		duration := time.Since(start).Seconds()

		code := status.Code(err).String()
		ClientRequestDuration.WithLabelValues(method, code).Observe(duration)
		ClientRequestsTotal.WithLabelValues(method, code).Inc()

		return err
	}
}

func StreamClientMetricInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		start := time.Now()
		clientStream, err := streamer(ctx, desc, cc, method, opts...)
		duration := time.Since(start).Seconds()

		code := status.Code(err).String()
		ClientRequestDuration.WithLabelValues(method, code).Observe(duration)
		ClientRequestsTotal.WithLabelValues(method, code).Inc()

		return clientStream, err
	}
}
