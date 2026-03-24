package gormx

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	dbRequestDuration *prometheus.HistogramVec
	dbRequestsTotal   *prometheus.CounterVec
}

var (
	defaultMetricsOnce sync.Once
	defaultMetrics     *metrics
)

func defaultGORMMetrics() *metrics {
	defaultMetricsOnce.Do(func() {
		defaultMetrics = newGORMMetrics(prometheus.DefaultRegisterer)
	})
	return defaultMetrics
}

func newGORMMetrics(registerer prometheus.Registerer) *metrics {
	m := &metrics{
		dbRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gormx_request_duration_seconds",
				Help:    "Database request duration in seconds.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"db", "operation", "status", "table"},
		),
		dbRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gormx_requests_total",
				Help: "Total number of database requests.",
			},
			[]string{"db", "operation", "status", "table"},
		),
	}

	mustRegisterCollector(registerer, &m.dbRequestDuration, m.dbRequestDuration)
	mustRegisterCollector(registerer, &m.dbRequestsTotal, m.dbRequestsTotal)

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
