package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns the Prometheus scrape endpoint handler for the default registry.
func Handler() http.Handler {
	return promhttp.Handler()
}
