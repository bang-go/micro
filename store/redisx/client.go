package redisx

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

var (
	RedisRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "micro_redis_request_duration_seconds",
			Help:    "Redis request duration in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"addr", "command", "status"},
	)

	RedisRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "micro_redis_requests_total",
			Help: "Redis requests total",
		},
		[]string{"addr", "command", "status"},
	)
)

func init() {
	prometheus.MustRegister(RedisRequestDuration)
	prometheus.MustRegister(RedisRequestsTotal)
}

type Config struct {
	Addr            string
	Password        string
	DB              int
	PoolSize        int
	MinIdleConns    int
	DialTimeout     time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	Protocol        int
	DisableIdentity bool
	Trace           bool
	Logger          *logger.Logger
	EnableLogger    bool
}

func New(conf *Config) *redis.Client {
	if conf == nil {
		conf = &Config{}
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}

	opt := &redis.Options{
		Addr:            conf.Addr,
		Password:        conf.Password,
		DB:              conf.DB,
		PoolSize:        conf.PoolSize,
		MinIdleConns:    conf.MinIdleConns,
		DialTimeout:     conf.DialTimeout,
		ReadTimeout:     conf.ReadTimeout,
		WriteTimeout:    conf.WriteTimeout,
		Protocol:        conf.Protocol,
		DisableIdentity: conf.DisableIdentity,
	}

	// Default timeouts if not set
	if opt.DialTimeout == 0 {
		opt.DialTimeout = 5 * time.Second
	}
	if opt.ReadTimeout == 0 {
		opt.ReadTimeout = 3 * time.Second
	}
	if opt.WriteTimeout == 0 {
		opt.WriteTimeout = 3 * time.Second
	}

	rdb := redis.NewClient(opt)

	if conf.Trace {
		if err := redisotel.InstrumentTracing(rdb); err != nil {
			conf.Logger.Error(context.Background(), "redis_trace_init_failed", "error", err)
		}
	}

	// Add Hook for Observability
	rdb.AddHook(&hook{
		addr:         conf.Addr,
		logger:       conf.Logger,
		enableLogger: conf.EnableLogger,
	})

	return rdb
}

type hook struct {
	addr         string
	logger       *logger.Logger
	enableLogger bool
}

func (h *hook) info(ctx context.Context, msg string, args ...any) {
	if h.enableLogger {
		h.logger.Info(ctx, msg, args...)
	}
}

func (h *hook) error(ctx context.Context, msg string, args ...any) {
	h.logger.Error(ctx, msg, args...)
}

func (h *hook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (h *hook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		duration := time.Since(start).Seconds()

		status := "success"
		if err != nil && !errors.Is(err, redis.Nil) {
			status = "error"
		}

		// Metrics
		RedisRequestDuration.WithLabelValues(h.addr, cmd.Name(), status).Observe(duration)
		RedisRequestsTotal.WithLabelValues(h.addr, cmd.Name(), status).Inc()

		// Logging
		if err != nil && !errors.Is(err, redis.Nil) {
			h.error(ctx, "redis_command_failed",
				"addr", h.addr,
				"command", cmd.Name(),
				"args", cmd.Args(),
				"error", err,
				"cost", duration,
			)
		} else {
			h.info(ctx, "redis_access_log",
				"addr", h.addr,
				"command", cmd.Name(),
				"status", status,
				"cost", duration,
			)
		}

		return err
	}
}

func (h *hook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		duration := time.Since(start).Seconds()

		status := "success"
		if err != nil && !errors.Is(err, redis.Nil) {
			status = "error"
		}

		// Metrics
		RedisRequestDuration.WithLabelValues(h.addr, "pipeline", status).Observe(duration)
		RedisRequestsTotal.WithLabelValues(h.addr, "pipeline", status).Inc()

		// Logging
		if err != nil && !errors.Is(err, redis.Nil) {
			h.error(ctx, "redis_pipeline_failed",
				"addr", h.addr,
				"count", len(cmds),
				"error", err,
				"cost", duration,
			)
		} else {
			h.info(ctx, "redis_pipeline_access_log",
				"addr", h.addr,
				"count", len(cmds),
				"status", status,
				"cost", duration,
			)
		}

		return err
	}
}
