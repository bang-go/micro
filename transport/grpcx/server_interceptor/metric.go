package grpcx

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	ServerRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grpc_server_request_duration_seconds",
			Help:    "gRPC server request duration in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"method", "code"},
	)

	ServerRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_requests_total",
			Help: "gRPC server requests total",
		},
		[]string{"method", "code"},
	)
)

func init() {
	prometheus.MustRegister(ServerRequestDuration)
	prometheus.MustRegister(ServerRequestsTotal)
}

func UnaryServerMetricInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start).Seconds()

		code := status.Code(err).String()
		ServerRequestDuration.WithLabelValues(info.FullMethod, code).Observe(duration)
		ServerRequestsTotal.WithLabelValues(info.FullMethod, code).Inc()

		return resp, err
	}
}

func StreamServerMetricInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, stream)
		duration := time.Since(start).Seconds()

		code := status.Code(err).String()
		ServerRequestDuration.WithLabelValues(info.FullMethod, code).Observe(duration)
		ServerRequestsTotal.WithLabelValues(info.FullMethod, code).Inc()

		return err
	}
}
