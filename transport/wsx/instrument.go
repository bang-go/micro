package wsx

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// Active connections count
	connActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_connections_active",
		Help: "Current number of active websocket connections",
	})

	// Total messages received from clients
	msgReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_messages_received_total",
		Help: "Total number of messages received from clients",
	})

	// Total messages sent to clients (broadcast or direct)
	// Label: status = "success" | "dropped" | "error"
	msgSent = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_messages_sent_total",
		Help: "Total number of messages sent to clients",
	}, []string{"status"})

	// Hub broadcast events
	hubBroadcast = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_hub_broadcast_total",
		Help: "Total number of broadcast events processed by hub",
	})
)

func init() {
	// Register metrics with Prometheus default registry
	// If you use a custom registry, you might want to export a Register function instead.
	// For simplicity/convention, we register to default.
	prometheus.MustRegister(connActive)
	prometheus.MustRegister(msgReceived)
	prometheus.MustRegister(msgSent)
	prometheus.MustRegister(hubBroadcast)
}
