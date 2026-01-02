package ginx

import (
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/gin-gonic/gin"
)

// LoggerMiddleware returns a gin.HandlerFunc (middleware) that logs requests using micro/logger
func LoggerMiddleware(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		end := time.Now()
		latency := end.Sub(start)

		if len(c.Errors) > 0 {
			for _, e := range c.Errors.Errors() {
				log.Error(c.Request.Context(), e)
			}
		} else {
			log.Info(c.Request.Context(), "access_log",
				"status", c.Writer.Status(),
				"method", c.Request.Method,
				"path", path,
				"query", query,
				"ip", c.ClientIP(),
				"user-agent", c.Request.UserAgent(),
				"cost", latency.Seconds(),
			)
		}
	}
}
