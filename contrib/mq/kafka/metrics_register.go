package kafka

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	consumerRequestsTotal *prometheus.CounterVec
	consumerDuration      *prometheus.HistogramVec
	consumerMessagesTotal *prometheus.CounterVec
}

var (
	defaultMetricsOnce sync.Once
	defaultMetrics     *metrics
)

func defaultKafkaMetrics() *metrics {
	defaultMetricsOnce.Do(func() {
		defaultMetrics = newKafkaMetrics(prometheus.DefaultRegisterer)
	})
	return defaultMetrics
}

func newKafkaMetrics(registerer prometheus.Registerer) *metrics {
	m := &metrics{
		consumerRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kafka_consumer_requests_total",
				Help: "Total number of Kafka consumer requests.",
			},
			[]string{"name", "operation", "status"},
		),
		consumerDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "kafka_consumer_request_duration_seconds",
				Help:    "Kafka consumer request duration in seconds.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"name", "operation", "status"},
		),
		consumerMessagesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kafka_consumer_messages_total",
				Help: "Total number of Kafka consumer messages received.",
			},
			[]string{"name", "status"},
		),
	}

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
