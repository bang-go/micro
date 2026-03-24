package redisx

import (
	"context"
	"crypto/tls"
	"strings"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/redis/go-redis/v9"
)

func normalizeContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func defaultLogger(log *logger.Logger) *logger.Logger {
	if log != nil {
		return log
	}
	return logger.New(logger.WithLevel("info"))
}

func cloneOptions(src *redis.Options) *redis.Options {
	if src == nil {
		return nil
	}
	cloned := *src
	if src.TLSConfig != nil {
		cloned.TLSConfig = src.TLSConfig.Clone()
	}
	return &cloned
}

func timeoutContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx = normalizeContext(ctx)
	if timeout <= 0 {
		return ctx, func() {}
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func cloneTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		return nil
	}
	return cfg.Clone()
}

func defaultRedisNetwork(addr string) string {
	if strings.HasPrefix(addr, "/") {
		return "unix"
	}
	return "tcp"
}

func commandName(cmd redis.Cmder) string {
	if cmd == nil {
		return ""
	}

	name := strings.TrimSpace(strings.ToLower(cmd.FullName()))
	if name != "" {
		return name
	}

	return strings.TrimSpace(strings.ToLower(cmd.Name()))
}

func isConnectionManagementCommand(cmd redis.Cmder) bool {
	switch commandName(cmd) {
	case "auth", "hello", "client setinfo", "client setname":
		return true
	default:
		return false
	}
}

func isConnectionManagementPipeline(cmds []redis.Cmder) bool {
	seen := false
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		seen = true
		if !isConnectionManagementCommand(cmd) {
			return false
		}
	}
	return seen
}
