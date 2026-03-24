package middleware

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/gin-gonic/gin"
)

func RecoveryMiddleware(log *logger.Logger) gin.HandlerFunc {
	if log == nil {
		log = logger.New(logger.WithLevel("info"))
	}

	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				errValue := recoveredError(recovered)

				if isBrokenPipe(errValue) {
					log.Error(c.Request.Context(), "http_server_connection_error",
						"error", errValue,
						"method", c.Request.Method,
						"path", c.Request.URL.Path,
						"route", routeLabel(c),
						"remote_addr", c.ClientIP(),
					)
					_ = c.Error(errValue)
					c.Abort()
					return
				}

				_ = c.Error(errValue)
				log.Error(c.Request.Context(), "http_server_panic_recovered",
					"error", errValue,
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
					"route", routeLabel(c),
					"remote_addr", c.ClientIP(),
					"stack", string(debug.Stack()),
				)
				if !c.Writer.Written() {
					c.AbortWithStatus(http.StatusInternalServerError)
					return
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

func recoveredError(recovered any) error {
	if err, ok := recovered.(error); ok {
		return err
	}
	return errors.New(fmt.Sprint(recovered))
}

func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}
	var netErr *net.OpError
	if !errors.As(err, &netErr) {
		return false
	}

	var syscallErr *os.SyscallError
	if !errors.As(netErr.Err, &syscallErr) {
		return false
	}

	message := strings.ToLower(syscallErr.Error())
	return strings.Contains(message, "broken pipe") || strings.Contains(message, "connection reset by peer")
}
