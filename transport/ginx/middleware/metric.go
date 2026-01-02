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
)

func init() {
	// Register metrics
	prometheus.MustRegister(RequestDurationHistogram)
	prometheus.MustRegister(RequestCounter)
}

// MetricMiddleware returns a gin.HandlerFunc (middleware) that records metrics
func MetricMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method
		path := c.FullPath() // 使用 FullPath 避免 URL 参数导致的高基数问题
		if path == "" {
			path = "unknown"
		}

		RequestDurationHistogram.WithLabelValues(method, path, status).Observe(duration)
		RequestCounter.WithLabelValues(method, path, status).Inc()
	}
}
