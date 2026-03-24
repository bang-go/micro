package udpx

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	clientDialDuration    *prometheus.HistogramVec
	clientDialsTotal      *prometheus.CounterVec
	serverPacketsReceived *prometheus.CounterVec
	serverPacketsHandled  *prometheus.CounterVec
	serverPacketsDropped  *prometheus.CounterVec
	serverPacketDuration  *prometheus.HistogramVec
	serverBytesRead       *prometheus.CounterVec
	serverBytesWritten    *prometheus.CounterVec
}

var (
	defaultMetricsOnce sync.Once
	defaultMetrics     *metrics
)

func defaultUDPMetrics() *metrics {
	defaultMetricsOnce.Do(func() {
		defaultMetrics = newUDPMetrics(prometheus.DefaultRegisterer)
	})
	return defaultMetrics
}

func newUDPMetrics(registerer prometheus.Registerer) *metrics {
	m := &metrics{
		clientDialDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "udpx_client_dial_duration_seconds",
				Help:    "UDP client dial duration in seconds.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"result"},
		),
		clientDialsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "udpx_client_dials_total",
				Help: "Total number of UDP client dial attempts.",
			},
			[]string{"result"},
		),
		serverPacketsReceived: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "udpx_server_packets_received_total",
				Help: "Total number of UDP packets read by the server.",
			},
			[]string{"addr"},
		),
		serverPacketsHandled: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "udpx_server_packets_handled_total",
				Help: "Total number of UDP packets processed by the server.",
			},
			[]string{"addr", "result"},
		),
		serverPacketsDropped: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "udpx_server_packets_dropped_total",
				Help: "Total number of UDP packets dropped by the server.",
			},
			[]string{"addr", "reason"},
		),
		serverPacketDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "udpx_server_packet_duration_seconds",
				Help:    "UDP packet handling duration in seconds.",
				Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
			},
			[]string{"addr", "result"},
		),
		serverBytesRead: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "udpx_server_bytes_read_total",
				Help: "Total bytes read by the UDP server.",
			},
			[]string{"addr"},
		),
		serverBytesWritten: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "udpx_server_bytes_written_total",
				Help: "Total bytes written by the UDP server.",
			},
			[]string{"addr"},
		),
	}

	mustRegisterCollector(registerer, &m.clientDialDuration, m.clientDialDuration)
	mustRegisterCollector(registerer, &m.clientDialsTotal, m.clientDialsTotal)
	mustRegisterCollector(registerer, &m.serverPacketsReceived, m.serverPacketsReceived)
	mustRegisterCollector(registerer, &m.serverPacketsHandled, m.serverPacketsHandled)
	mustRegisterCollector(registerer, &m.serverPacketsDropped, m.serverPacketsDropped)
	mustRegisterCollector(registerer, &m.serverPacketDuration, m.serverPacketDuration)
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
