package middleware

import (
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/gin-gonic/gin"
)

func LoggerMiddleware(log *logger.Logger, skipPaths ...string) gin.HandlerFunc {
	if log == nil {
		log = logger.New(logger.WithLevel("info"))
	}
	skip := newSkipPathSet(skipPaths)

	return func(c *gin.Context) {
		if shouldSkip(skip, c.Request.URL.Path) {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()

		fields := []any{
			"status", c.Writer.Status(),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"route", routeLabel(c),
			"bytes", bytesWritten(c.Writer.Size()),
			"remote_addr", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
			"duration", time.Since(start).Seconds(),
		}
		if len(c.Errors) > 0 {
			fields = append(fields, "errors", c.Errors.String())
		}

		status := c.Writer.Status()
		if status >= 500 {
			log.Error(c.Request.Context(), "http_server_request", fields...)
			return
		}
		if status >= 400 || len(c.Errors) > 0 {
			log.Warn(c.Request.Context(), "http_server_request", fields...)
			return
		}

		log.Info(c.Request.Context(), "http_server_request", fields...)
	}
}
