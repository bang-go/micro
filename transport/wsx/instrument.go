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

	// Hub kick events
	hubKick = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_hub_kick_total",
		Help: "Total number of kick events processed by hub",
	})

	// Hub room operations
	// Label: op = "join" | "leave" | "broadcast"
	hubRoomOps = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_hub_room_ops_total",
		Help: "Total number of room operations",
	}, []string{"op"})

	// Hub command acknowledgements.
	// Label: scope = "hub" | "room", status = "success" | "error"
	hubCommandAcks = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_hub_command_ack_total",
		Help: "Total number of distributed hub command acknowledgements",
	}, []string{"scope", "status"})

	// Redis broker internal errors that cannot be returned to a caller.
	// Label: op = "receive" | "unsubscribe"
	redisBrokerErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_redis_broker_errors_total",
		Help: "Total number of Redis broker internal errors",
	}, []string{"op"})

	// Limit exceeded events
	// Label: type = "max_rooms"
	limitExceeded = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_limit_exceeded_total",
		Help: "Total number of limit exceeded events",
	}, []string{"type"})
)
