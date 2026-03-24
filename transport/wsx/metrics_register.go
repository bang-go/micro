package wsx

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var registerMetricsOnce sync.Once

func registerWSMetrics() {
	registerMetricsOnce.Do(func() {
		mustRegisterCollector(&connActive, connActive)
		mustRegisterCollector(&msgReceived, msgReceived)
		mustRegisterCollector(&msgSent, msgSent)
		mustRegisterCollector(&hubBroadcast, hubBroadcast)
		mustRegisterCollector(&hubKick, hubKick)
		mustRegisterCollector(&hubRoomOps, hubRoomOps)
		mustRegisterCollector(&limitExceeded, limitExceeded)
	})
}

func mustRegisterCollector[T prometheus.Collector](dst *T, collector T) {
	if err := prometheus.Register(collector); err != nil {
		if alreadyRegistered, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if registered, ok := alreadyRegistered.ExistingCollector.(T); ok {
				*dst = registered
				return
			}
		}

		panic(err)
	}
}
