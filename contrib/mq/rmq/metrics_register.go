package rmq

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	producerRequestsTotal *prometheus.CounterVec
	producerDuration      *prometheus.HistogramVec
	consumerRequestsTotal *prometheus.CounterVec
	consumerDuration      *prometheus.HistogramVec
	consumerMessagesTotal *prometheus.CounterVec
}

var (
	defaultMetricsOnce sync.Once
	defaultMetrics     *metrics
)

func defaultRMQMetrics() *metrics {
	defaultMetricsOnce.Do(func() {
		defaultMetrics = newRMQMetrics(prometheus.DefaultRegisterer)
	})
	return defaultMetrics
}

func newRMQMetrics(registerer prometheus.Registerer) *metrics {
	m := &metrics{
		producerRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rmq_producer_requests_total",
				Help: "Total number of RocketMQ producer requests.",
			},
			[]string{"name", "operation", "status"},
		),
		producerDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "rmq_producer_request_duration_seconds",
				Help:    "RocketMQ producer request duration in seconds.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"name", "operation", "status"},
		),
		consumerRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rmq_consumer_requests_total",
				Help: "Total number of RocketMQ consumer requests.",
			},
			[]string{"name", "operation", "status"},
		),
		consumerDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "rmq_consumer_request_duration_seconds",
				Help:    "RocketMQ consumer request duration in seconds.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"name", "operation", "status"},
		),
		consumerMessagesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rmq_consumer_messages_total",
				Help: "Total number of RocketMQ consumer messages received.",
			},
			[]string{"name", "status"},
		),
	}

	mustRegisterCollector(registerer, &m.producerRequestsTotal, m.producerRequestsTotal)
	mustRegisterCollector(registerer, &m.producerDuration, m.producerDuration)
	mustRegisterCollector(registerer, &m.consumerRequestsTotal, m.consumerRequestsTotal)
	mustRegisterCollector(registerer, &m.consumerDuration, m.consumerDuration)
	mustRegisterCollector(registerer, &m.consumerMessagesTotal, m.consumerMessagesTotal)

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
