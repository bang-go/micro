package grpcx

import "github.com/prometheus/client_golang/prometheus"

func mustRegisterCollector[T prometheus.Collector](registerer prometheus.Registerer, dst *T, collector T) {
	if registerer == nil {
		return
	}

	if err := registerer.Register(collector); err != nil {
		if alreadyRegistered, ok := err.(prometheus.AlreadyRegisteredError); ok {
			registered, ok := alreadyRegistered.ExistingCollector.(T)
			if ok {
				*dst = registered
				return
			}
		}

		panic(err)
	}
}
