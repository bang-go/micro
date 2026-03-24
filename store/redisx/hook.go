package redisx

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/redis/go-redis/v9"
)

type observabilityHook struct {
	name          string
	addr          string
	logger        *logger.Logger
	enableLogger  bool
	slowThreshold time.Duration
	metrics       *metrics
}

func newObservabilityHook(conf *Config, addr string, metrics *metrics) redis.Hook {
	return &observabilityHook{
		name:          conf.Name,
		addr:          addr,
		logger:        conf.Logger,
		enableLogger:  conf.EnableLogger,
		slowThreshold: conf.SlowThreshold,
		metrics:       metrics,
	}
}

func (h *observabilityHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *observabilityHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		command := commandName(cmd)
		start := time.Now()
		err := next(ctx, cmd)
		if isConnectionManagementCommand(cmd) {
			return err
		}

		duration := time.Since(start)
		status := commandStatus(err)

		if h.metrics != nil {
			h.metrics.requestDuration.WithLabelValues(h.name, command, status).Observe(duration.Seconds())
			h.metrics.requestsTotal.WithLabelValues(h.name, command, status).Inc()
		}

		h.logCommand(ctx, command, len(cmd.Args())-1, status, duration, err)
		return err
	}
}

func (h *observabilityHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		if isConnectionManagementPipeline(cmds) {
			return err
		}

		duration := time.Since(start)
		status := pipelineStatus(err, cmds)
		commands := observedPipelineCommands(cmds)
		if len(commands) == 0 {
			return err
		}

		if h.metrics != nil {
			h.metrics.requestDuration.WithLabelValues(h.name, "pipeline", status).Observe(duration.Seconds())
			h.metrics.requestsTotal.WithLabelValues(h.name, "pipeline", status).Inc()
		}

		h.logPipeline(ctx, commands, observedPipelineCommandCount(cmds), status, duration, err)
		return err
	}
}

func (h *observabilityHook) logCommand(
	ctx context.Context,
	command string,
	argCount int,
	status string,
	duration time.Duration,
	err error,
) {
	fields := []any{
		"name", h.name,
		"addr", h.addr,
		"command", command,
		"arg_count", max(argCount, 0),
		"status", status,
		"duration", duration,
	}

	switch status {
	case "error":
		fields = append(fields, "error", err)
		h.logger.Error(normalizeContext(ctx), "redis command failed", fields...)
	case "not_found":
		if h.enableLogger {
			h.logger.Debug(normalizeContext(ctx), "redis command not found", fields...)
		}
	default:
		if h.enableLogger && h.slowThreshold > 0 && duration >= h.slowThreshold {
			h.logger.Warn(normalizeContext(ctx), "redis command slow", append(fields, "slow_threshold", h.slowThreshold)...)
			return
		}
		if h.enableLogger {
			h.logger.Debug(normalizeContext(ctx), "redis command completed", fields...)
		}
	}
}

func (h *observabilityHook) logPipeline(
	ctx context.Context,
	commands []string,
	commandCount int,
	status string,
	duration time.Duration,
	err error,
) {
	fields := []any{
		"name", h.name,
		"addr", h.addr,
		"commands", commands,
		"command_count", commandCount,
		"status", status,
		"duration", duration,
	}

	switch status {
	case "error":
		fields = append(fields, "error", err)
		h.logger.Error(normalizeContext(ctx), "redis pipeline failed", fields...)
	case "not_found":
		if h.enableLogger {
			h.logger.Debug(normalizeContext(ctx), "redis pipeline not found", fields...)
		}
	default:
		if h.enableLogger && h.slowThreshold > 0 && duration >= h.slowThreshold {
			h.logger.Warn(normalizeContext(ctx), "redis pipeline slow", append(fields, "slow_threshold", h.slowThreshold)...)
			return
		}
		if h.enableLogger {
			h.logger.Debug(normalizeContext(ctx), "redis pipeline completed", fields...)
		}
	}
}

func commandStatus(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, redis.Nil):
		return "not_found"
	default:
		return "error"
	}
}

func pipelineStatus(err error, cmds []redis.Cmder) string {
	status := commandStatus(err)
	if status == "error" || status == "not_found" {
		return status
	}

	foundNotFound := false
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		switch commandStatus(cmd.Err()) {
		case "error":
			return "error"
		case "not_found":
			foundNotFound = true
		}
	}
	if foundNotFound {
		return "not_found"
	}
	return "success"
}

func observedPipelineCommands(cmds []redis.Cmder) []string {
	names := make([]string, 0, len(cmds))
	seen := make(map[string]struct{}, len(cmds))
	for _, cmd := range cmds {
		if cmd == nil || isConnectionManagementCommand(cmd) {
			continue
		}
		name := commandName(cmd)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func observedPipelineCommandCount(cmds []redis.Cmder) int {
	count := 0
	for _, cmd := range cmds {
		if cmd == nil || isConnectionManagementCommand(cmd) {
			continue
		}
		count++
	}
	return count
}
