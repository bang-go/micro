package ginx

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// RequestDurationHistogram 记录请求耗时分布
	RequestDurationHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_server_request_duration_seconds",
			Help:    "HTTP server request duration in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path", "status"},
	)

	// RequestCounter 记录请求总数
	RequestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_server_requests_total",
			Help: "HTTP server requests total",
		},
		[]string{"method", "path", "status"},
	)
	// RequestInFlight 记录当前并发请求数
	RequestInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_server_requests_in_flight",
			Help: "HTTP server requests currently processing",
		},
		[]string{"method", "path"},
	)
)

func init() {
	// Register metrics
	prometheus.MustRegister(RequestDurationHistogram)
	prometheus.MustRegister(RequestCounter)
	prometheus.MustRegister(RequestInFlight)
}

// MetricMiddleware returns a gin.HandlerFunc (middleware) that records metrics
// skipPaths: paths to ignore
func MetricMiddleware(skipPaths ...string) gin.HandlerFunc {
	// Create a map for faster lookup
	skipMap := make(map[string]struct{}, len(skipPaths))
	for _, p := range skipPaths {
		skipMap[p] = struct{}{}
	}

	return func(c *gin.Context) {
		// Check if path should be skipped
		if _, ok := skipMap[c.FullPath()]; ok {
			c.Next()
			return
		}
		// Also check RequestURI for exact matches like /healthz if FullPath is not set or different
		if _, ok := skipMap[c.Request.URL.Path]; ok {
			c.Next()
			return
		}

		start := time.Now()
		method := c.Request.Method
		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}

		RequestInFlight.WithLabelValues(method, path).Inc()
		defer RequestInFlight.WithLabelValues(method, path).Dec()

		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())

		RequestDurationHistogram.WithLabelValues(method, path, status).Observe(duration)
		RequestCounter.WithLabelValues(method, path, status).Inc()
	}
}
