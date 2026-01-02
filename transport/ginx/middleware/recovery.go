package ginx

import (
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime/debug"
	"strings"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/gin-gonic/gin"
)

// RecoveryMiddleware returns a gin.HandlerFunc (middleware) that recovers from any panic and logs requests using micro/logger
func RecoveryMiddleware(log *logger.Logger, stack bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Check for a broken connection, as it is not really a
				// condition that warrants a panic stack trace.
				var brokenPipe bool
				if ne, ok := err.(*net.OpError); ok {
					if se, ok := ne.Err.(*os.SyscallError); ok {
						if strings.Contains(strings.ToLower(se.Error()), "broken pipe") || strings.Contains(strings.ToLower(se.Error()), "connection reset by peer") {
							brokenPipe = true
						}
					}
				}

				httpRequest, _ := httputil.DumpRequest(c.Request, false)
				if brokenPipe {
					log.Error(c.Request.Context(), c.Request.URL.Path,
						"error", err,
						"request", string(httpRequest),
					)
					// If the connection is dead, we can't write a status to it.
					_ = c.Error(err.(error)) // nolint: errcheck
					c.Abort()
					return
				}

				if stack {
					log.Error(c.Request.Context(), "[Recovery from panic]",
						"error", err,
						"request", string(httpRequest),
						"stack", string(debug.Stack()),
					)
				} else {
					log.Error(c.Request.Context(), "[Recovery from panic]",
						"error", err,
						"request", string(httpRequest),
					)
				}
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}
