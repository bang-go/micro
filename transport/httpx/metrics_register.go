package httpx

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	clientRequestDuration *prometheus.HistogramVec
	clientRequestsTotal   *prometheus.CounterVec
	serverRequestDuration *prometheus.HistogramVec
	serverRequestsTotal   *prometheus.CounterVec
}

var (
	defaultMetricsOnce sync.Once
	defaultMetrics     *metrics
)

func defaultHTTPMetrics() *metrics {
	defaultMetricsOnce.Do(func() {
		defaultMetrics = newHTTPMetrics(prometheus.DefaultRegisterer)
	})
	return defaultMetrics
}

func newHTTPMetrics(registerer prometheus.Registerer) *metrics {
	m := &metrics{
		clientRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "httpx_client_request_duration_seconds",
				Help:    "HTTP client request duration in seconds.",
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "code"},
		),
		clientRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "httpx_client_requests_total",
				Help: "Total number of HTTP client requests.",
			},
			[]string{"method", "code"},
		),
		serverRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "httpx_server_request_duration_seconds",
				Help:    "HTTP server request duration in seconds.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "code"},
		),
		serverRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "httpx_server_requests_total",
				Help: "Total number of HTTP server requests.",
			},
			[]string{"method", "code"},
		),
	}

	mustRegisterCollector(registerer, &m.clientRequestDuration, m.clientRequestDuration)
	mustRegisterCollector(registerer, &m.clientRequestsTotal, m.clientRequestsTotal)
	mustRegisterCollector(registerer, &m.serverRequestDuration, m.serverRequestDuration)
	mustRegisterCollector(registerer, &m.serverRequestsTotal, m.serverRequestsTotal)

	return m
}

func mustRegisterCollector[T prometheus.Collector](registerer prometheus.Registerer, dst *T, collector T) {
	if registerer == nil {
		return
	}
	if err := registerer.Register(collector); err != nil {
		if alreadyRegistered, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if registered, ok := alreadyRegistered.ExistingCollector.(T); ok {
				*dst = registered
				return
			}
		}
		panic(err)
	}
}
