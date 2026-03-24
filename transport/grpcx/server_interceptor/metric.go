package grpcx

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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

	ServerInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grpc_server_requests_in_flight",
			Help: "gRPC server requests currently processing",
		},
		[]string{"method"},
	)
)

type Metrics struct {
	RequestDuration *prometheus.HistogramVec
	RequestsTotal   *prometheus.CounterVec
	InFlight        *prometheus.GaugeVec
}

var (
	defaultMetricsOnce sync.Once
	defaultMetrics     *Metrics
)

func DefaultMetrics() *Metrics {
	defaultMetricsOnce.Do(func() {
		defaultMetrics = &Metrics{
			RequestDuration: ServerRequestDuration,
			RequestsTotal:   ServerRequestsTotal,
			InFlight:        ServerInFlight,
		}
		mustRegisterCollector(prometheus.DefaultRegisterer, &defaultMetrics.RequestDuration, defaultMetrics.RequestDuration)
		mustRegisterCollector(prometheus.DefaultRegisterer, &defaultMetrics.RequestsTotal, defaultMetrics.RequestsTotal)
		mustRegisterCollector(prometheus.DefaultRegisterer, &defaultMetrics.InFlight, defaultMetrics.InFlight)
		ServerRequestDuration = defaultMetrics.RequestDuration
		ServerRequestsTotal = defaultMetrics.RequestsTotal
		ServerInFlight = defaultMetrics.InFlight
	})

	return defaultMetrics
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	metrics := &Metrics{
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "grpc_server_request_duration_seconds",
				Help:    "gRPC server request duration in seconds",
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "code"},
		),
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "grpc_server_requests_total",
				Help: "gRPC server requests total",
			},
			[]string{"method", "code"},
		),
		InFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "grpc_server_requests_in_flight",
				Help: "gRPC server requests currently processing",
			},
			[]string{"method"},
		),
	}
	mustRegisterCollector(registerer, &metrics.RequestDuration, metrics.RequestDuration)
	mustRegisterCollector(registerer, &metrics.RequestsTotal, metrics.RequestsTotal)
	mustRegisterCollector(registerer, &metrics.InFlight, metrics.InFlight)
	return metrics
}

func UnaryServerMetricInterceptor(skipMethods ...string) grpc.UnaryServerInterceptor {
	return UnaryServerMetricInterceptorWithMetrics(DefaultMetrics(), skipMethods...)
}

func UnaryServerMetricInterceptorWithMetrics(metrics *Metrics, skipMethods ...string) grpc.UnaryServerInterceptor {
	if metrics == nil {
		metrics = DefaultMetrics()
	}

	skipMap := make(map[string]struct{}, len(skipMethods))
	for _, m := range skipMethods {
		skipMap[m] = struct{}{}
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if _, ok := skipMap[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		start := time.Now()
		metrics.InFlight.WithLabelValues(info.FullMethod).Inc()
		defer metrics.InFlight.WithLabelValues(info.FullMethod).Dec()

		resp, err := handler(ctx, req)
		duration := time.Since(start).Seconds()

		code := rpcStatusCode(err).String()
		metrics.RequestDuration.WithLabelValues(info.FullMethod, code).Observe(duration)
		metrics.RequestsTotal.WithLabelValues(info.FullMethod, code).Inc()

		return resp, err
	}
}

func StreamServerMetricInterceptor(skipMethods ...string) grpc.StreamServerInterceptor {
	return StreamServerMetricInterceptorWithMetrics(DefaultMetrics(), skipMethods...)
}

func StreamServerMetricInterceptorWithMetrics(metrics *Metrics, skipMethods ...string) grpc.StreamServerInterceptor {
	if metrics == nil {
		metrics = DefaultMetrics()
	}

	skipMap := make(map[string]struct{}, len(skipMethods))
	for _, m := range skipMethods {
		skipMap[m] = struct{}{}
	}

	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if _, ok := skipMap[info.FullMethod]; ok {
			return handler(srv, stream)
		}

		start := time.Now()
		metrics.InFlight.WithLabelValues(info.FullMethod).Inc()
		defer metrics.InFlight.WithLabelValues(info.FullMethod).Dec()

		err := handler(srv, stream)
		duration := time.Since(start).Seconds()

		code := rpcStatusCode(err).String()
		metrics.RequestDuration.WithLabelValues(info.FullMethod, code).Observe(duration)
		metrics.RequestsTotal.WithLabelValues(info.FullMethod, code).Inc()

		return err
	}
}

func rpcStatusCode(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Code()
	}
	return status.Code(err)
}
