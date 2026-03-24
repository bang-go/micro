package middleware

import (
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	requestDurationHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ginx_server_request_duration_seconds",
			Help:    "Gin HTTP server request duration in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"method", "route", "status"},
	)
	requestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ginx_server_requests_total",
			Help: "Total number of Gin HTTP server requests.",
		},
		[]string{"method", "route", "status"},
	)
	requestInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ginx_server_requests_in_flight",
			Help: "Current number of in-flight Gin HTTP server requests.",
		},
		[]string{"method", "route"},
	)
)

type Metrics struct {
	RequestDuration  *prometheus.HistogramVec
	RequestsTotal    *prometheus.CounterVec
	RequestsInFlight *prometheus.GaugeVec
}

var (
	defaultMetricsOnce sync.Once
	defaultMetrics     *Metrics
)

func DefaultMetrics() *Metrics {
	defaultMetricsOnce.Do(func() {
		defaultMetrics = &Metrics{
			RequestDuration:  requestDurationHistogram,
			RequestsTotal:    requestCounter,
			RequestsInFlight: requestInFlight,
		}
		mustRegisterCollector(prometheus.DefaultRegisterer, &defaultMetrics.RequestDuration, defaultMetrics.RequestDuration)
		mustRegisterCollector(prometheus.DefaultRegisterer, &defaultMetrics.RequestsTotal, defaultMetrics.RequestsTotal)
		mustRegisterCollector(prometheus.DefaultRegisterer, &defaultMetrics.RequestsInFlight, defaultMetrics.RequestsInFlight)
		requestDurationHistogram = defaultMetrics.RequestDuration
		requestCounter = defaultMetrics.RequestsTotal
		requestInFlight = defaultMetrics.RequestsInFlight
	})

	return defaultMetrics
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	metrics := &Metrics{
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ginx_server_request_duration_seconds",
				Help:    "Gin HTTP server request duration in seconds.",
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "route", "status"},
		),
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ginx_server_requests_total",
				Help: "Total number of Gin HTTP server requests.",
			},
			[]string{"method", "route", "status"},
		),
		RequestsInFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "ginx_server_requests_in_flight",
				Help: "Current number of in-flight Gin HTTP server requests.",
			},
			[]string{"method", "route"},
		),
	}
	mustRegisterCollector(registerer, &metrics.RequestDuration, metrics.RequestDuration)
	mustRegisterCollector(registerer, &metrics.RequestsTotal, metrics.RequestsTotal)
	mustRegisterCollector(registerer, &metrics.RequestsInFlight, metrics.RequestsInFlight)
	return metrics
}

func MetricMiddleware(skipPaths ...string) gin.HandlerFunc {
	return MetricMiddlewareWithMetrics(DefaultMetrics(), skipPaths...)
}

func MetricMiddlewareWithMetrics(metrics *Metrics, skipPaths ...string) gin.HandlerFunc {
	if metrics == nil {
		metrics = DefaultMetrics()
	}
	skipMap := newSkipPathSet(skipPaths)

	return func(c *gin.Context) {
		if shouldSkip(skipMap, c.Request.URL.Path) {
			c.Next()
			return
		}

		start := time.Now()
		method := c.Request.Method
		route := routeLabel(c)

		metrics.RequestsInFlight.WithLabelValues(method, route).Inc()
		defer metrics.RequestsInFlight.WithLabelValues(method, route).Dec()

		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())
		route = routeLabel(c)

		metrics.RequestDuration.WithLabelValues(method, route, status).Observe(duration)
		metrics.RequestsTotal.WithLabelValues(method, route, status).Inc()
	}
}
