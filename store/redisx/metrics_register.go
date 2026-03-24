package redisx

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	requestDuration *prometheus.HistogramVec
	requestsTotal   *prometheus.CounterVec
}

var (
	defaultMetricsOnce sync.Once
	defaultMetrics     *metrics
)

func defaultRedisMetrics() *metrics {
	defaultMetricsOnce.Do(func() {
		defaultMetrics = newRedisMetrics(prometheus.DefaultRegisterer)
	})
	return defaultMetrics
}

func newRedisMetrics(registerer prometheus.Registerer) *metrics {
	m := &metrics{
		requestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "redisx_request_duration_seconds",
				Help:    "Redis request duration in seconds.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"name", "command", "status"},
		),
		requestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "redisx_requests_total",
				Help: "Total number of Redis requests.",
			},
			[]string{"name", "command", "status"},
		),
	}

	mustRegisterCollector(registerer, &m.requestDuration, m.requestDuration)
	mustRegisterCollector(registerer, &m.requestsTotal, m.requestsTotal)

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
