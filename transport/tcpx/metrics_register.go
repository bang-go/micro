package tcpx

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	clientDialDuration        *prometheus.HistogramVec
	clientDialsTotal          *prometheus.CounterVec
	serverConnectionsActive   *prometheus.GaugeVec
	serverConnectionsAccepted *prometheus.CounterVec
	serverConnectionsClosed   *prometheus.CounterVec
	serverConnectionsRejected *prometheus.CounterVec
	serverConnectionDuration  *prometheus.HistogramVec
	serverBytesRead           *prometheus.CounterVec
	serverBytesWritten        *prometheus.CounterVec
}

var (
	defaultMetricsOnce sync.Once
	defaultMetrics     *metrics
)

func defaultTCPMetrics() *metrics {
	defaultMetricsOnce.Do(func() {
		defaultMetrics = newTCPMetrics(prometheus.DefaultRegisterer)
	})
	return defaultMetrics
}

func newTCPMetrics(registerer prometheus.Registerer) *metrics {
	m := &metrics{
		clientDialDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tcpx_client_dial_duration_seconds",
				Help:    "TCP client dial duration in seconds.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"result"},
		),
		clientDialsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tcpx_client_dials_total",
				Help: "Total number of TCP client dial attempts.",
			},
			[]string{"result"},
		),
		serverConnectionsActive: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tcpx_server_connections_active",
				Help: "Current number of active TCP server connections.",
			},
			[]string{"addr"},
		),
		serverConnectionsAccepted: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tcpx_server_connections_accepted_total",
				Help: "Total number of accepted TCP server connections.",
			},
			[]string{"addr"},
		),
		serverConnectionsClosed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tcpx_server_connections_closed_total",
				Help: "Total number of closed TCP server connections.",
			},
			[]string{"addr", "result"},
		),
		serverConnectionsRejected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tcpx_server_connections_rejected_total",
				Help: "Total number of rejected TCP server connections.",
			},
			[]string{"addr", "reason"},
		),
		serverConnectionDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tcpx_server_connection_duration_seconds",
				Help:    "TCP server connection duration in seconds.",
				Buckets: []float64{0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 300},
			},
			[]string{"addr", "result"},
		),
		serverBytesRead: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tcpx_server_bytes_read_total",
				Help: "Total bytes read by the TCP server.",
			},
			[]string{"addr"},
		),
		serverBytesWritten: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tcpx_server_bytes_written_total",
				Help: "Total bytes written by the TCP server.",
			},
			[]string{"addr"},
		),
	}

	mustRegisterCollector(registerer, &m.clientDialDuration, m.clientDialDuration)
	mustRegisterCollector(registerer, &m.clientDialsTotal, m.clientDialsTotal)
	mustRegisterCollector(registerer, &m.serverConnectionsActive, m.serverConnectionsActive)
	mustRegisterCollector(registerer, &m.serverConnectionsAccepted, m.serverConnectionsAccepted)
	mustRegisterCollector(registerer, &m.serverConnectionsClosed, m.serverConnectionsClosed)
	mustRegisterCollector(registerer, &m.serverConnectionsRejected, m.serverConnectionsRejected)
	mustRegisterCollector(registerer, &m.serverConnectionDuration, m.serverConnectionDuration)
	mustRegisterCollector(registerer, &m.serverBytesRead, m.serverBytesRead)
	mustRegisterCollector(registerer, &m.serverBytesWritten, m.serverBytesWritten)

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
