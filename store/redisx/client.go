package redisx

import (
	"context"
	"crypto/tls"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultDialTimeout   = 5 * time.Second
	defaultReadTimeout   = 3 * time.Second
	defaultWriteTimeout  = 3 * time.Second
	defaultPingTimeout   = 5 * time.Second
	defaultSlowThreshold = 250 * time.Millisecond
)

type Config struct {
	Name string

	Options *redis.Options

	Network               string
	Addr                  string
	Username              string
	Password              string
	DB                    int
	ClientName            string
	Protocol              int
	Dialer                func(context.Context, string, string) (net.Conn, error)
	OnConnect             func(context.Context, *redis.Conn) error
	MaxRetries            int
	MinRetryBackoff       time.Duration
	MaxRetryBackoff       time.Duration
	DialTimeout           time.Duration
	ReadTimeout           time.Duration
	WriteTimeout          time.Duration
	ContextTimeoutEnabled bool
	ReadBufferSize        int
	WriteBufferSize       int
	PoolFIFO              bool
	PoolSize              int
	MinIdleConns          int
	MaxIdleConns          int
	MaxActiveConns        int
	PoolTimeout           time.Duration
	ConnMaxIdleTime       time.Duration
	ConnMaxLifetime       time.Duration
	TLSConfig             *tls.Config
	DisableIdentity       *bool
	IdentitySuffix        string

	SkipPing      bool
	PingTimeout   time.Duration
	SlowThreshold time.Duration

	Trace                   bool
	TraceProvider           trace.TracerProvider
	TraceAttributes         []attribute.KeyValue
	TraceIncludeCommandArgs bool
	TraceCaller             bool

	Logger            *logger.Logger
	EnableLogger      bool
	DisableMetrics    bool
	MetricsRegisterer prometheus.Registerer
}

type Client interface {
	Redis() *redis.Client
	Options() *redis.Options
	Ping(context.Context) error
	Stats() redis.PoolStats
	AddHook(redis.Hook) error
	Close() error
}

type clientEntity struct {
	client    *redis.Client
	options   *redis.Options
	closeOnce sync.Once
	closeErr  error
}

func Open(ctx context.Context, conf *Config) (Client, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if conf == nil {
		return nil, ErrNilConfig
	}

	config, opts, err := prepareConfig(conf)
	if err != nil {
		return nil, err
	}

	var metrics *metrics
	if !config.DisableMetrics {
		metrics = defaultRedisMetrics()
		if config.MetricsRegisterer != nil {
			metrics = newRedisMetrics(config.MetricsRegisterer)
		}
	}

	rdb := redis.NewClient(opts)

	client := &clientEntity{
		client:  rdb,
		options: cloneOptions(rdb.Options()),
	}

	if !config.SkipPing {
		pingCtx, cancel := timeoutContext(ctx, config.PingTimeout)
		defer cancel()
		if err := client.Ping(pingCtx); err != nil {
			_ = client.Close()
			return nil, err
		}
	}

	rdb.AddHook(newObservabilityHook(config, opts.Addr, metrics))

	if config.Trace {
		if err := redisotel.InstrumentTracing(rdb, buildTraceOptions(config)...); err != nil {
			_ = client.Close()
			return nil, err
		}
	}

	return client, nil
}

func New(conf *Config) (Client, error) {
	return Open(context.Background(), conf)
}

func (c *clientEntity) Redis() *redis.Client {
	return c.client
}

func (c *clientEntity) Options() *redis.Options {
	return cloneOptions(c.options)
}

func (c *clientEntity) Ping(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	return c.client.Ping(ctx).Err()
}

func (c *clientEntity) Stats() redis.PoolStats {
	stats := c.client.PoolStats()
	if stats == nil {
		return redis.PoolStats{}
	}
	return *stats
}

func (c *clientEntity) AddHook(hook redis.Hook) error {
	if hook == nil {
		return ErrNilHook
	}
	c.client.AddHook(hook)
	return nil
}

func (c *clientEntity) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.client.Close()
	})
	return c.closeErr
}

func prepareConfig(conf *Config) (*Config, *redis.Options, error) {
	cloned := *conf
	cloned.Name = strings.TrimSpace(cloned.Name)
	cloned.Network = strings.TrimSpace(cloned.Network)
	cloned.Addr = strings.TrimSpace(cloned.Addr)
	cloned.Username = strings.TrimSpace(cloned.Username)
	cloned.ClientName = strings.TrimSpace(cloned.ClientName)
	cloned.IdentitySuffix = strings.TrimSpace(cloned.IdentitySuffix)
	cloned.TraceAttributes = append([]attribute.KeyValue(nil), cloned.TraceAttributes...)
	cloned.Logger = defaultLogger(cloned.Logger)
	if cloned.PingTimeout == 0 {
		cloned.PingTimeout = defaultPingTimeout
	}
	if cloned.SlowThreshold == 0 {
		cloned.SlowThreshold = defaultSlowThreshold
	}

	var opts *redis.Options
	if cloned.Options != nil {
		opts = cloneOptions(cloned.Options)
		if cloned.DisableIdentity != nil {
			opts.DisableIdentity = *cloned.DisableIdentity
		}
	} else {
		opts = &redis.Options{
			Network:               cloned.Network,
			Addr:                  cloned.Addr,
			Username:              cloned.Username,
			Password:              cloned.Password,
			DB:                    cloned.DB,
			ClientName:            cloned.ClientName,
			Protocol:              cloned.Protocol,
			Dialer:                cloned.Dialer,
			OnConnect:             cloned.OnConnect,
			MaxRetries:            cloned.MaxRetries,
			MinRetryBackoff:       cloned.MinRetryBackoff,
			MaxRetryBackoff:       cloned.MaxRetryBackoff,
			DialTimeout:           cloned.DialTimeout,
			ReadTimeout:           cloned.ReadTimeout,
			WriteTimeout:          cloned.WriteTimeout,
			ContextTimeoutEnabled: cloned.ContextTimeoutEnabled,
			ReadBufferSize:        cloned.ReadBufferSize,
			WriteBufferSize:       cloned.WriteBufferSize,
			PoolFIFO:              cloned.PoolFIFO,
			PoolSize:              cloned.PoolSize,
			MinIdleConns:          cloned.MinIdleConns,
			MaxIdleConns:          cloned.MaxIdleConns,
			MaxActiveConns:        cloned.MaxActiveConns,
			PoolTimeout:           cloned.PoolTimeout,
			ConnMaxIdleTime:       cloned.ConnMaxIdleTime,
			ConnMaxLifetime:       cloned.ConnMaxLifetime,
			TLSConfig:             cloneTLSConfig(cloned.TLSConfig),
			DisableIdentity:       boolValue(cloned.DisableIdentity, true),
			IdentitySuffix:        cloned.IdentitySuffix,
		}
	}
	opts.Network = strings.TrimSpace(opts.Network)
	opts.Addr = strings.TrimSpace(opts.Addr)
	opts.Username = strings.TrimSpace(opts.Username)
	opts.ClientName = strings.TrimSpace(opts.ClientName)
	opts.IdentitySuffix = strings.TrimSpace(opts.IdentitySuffix)

	if opts.Addr == "" {
		return nil, nil, ErrAddrRequired
	}
	if opts.Network == "" {
		opts.Network = defaultRedisNetwork(opts.Addr)
	}
	if opts.DialTimeout == 0 {
		opts.DialTimeout = defaultDialTimeout
	}
	if opts.ReadTimeout == 0 {
		opts.ReadTimeout = defaultReadTimeout
	}
	if opts.WriteTimeout == 0 {
		opts.WriteTimeout = defaultWriteTimeout
	}
	if cloned.Name == "" {
		cloned.Name = opts.Addr
	}
	return &cloned, opts, nil
}

func buildTraceOptions(conf *Config) []redisotel.TracingOption {
	opts := make([]redisotel.TracingOption, 0, 4)
	if conf.TraceProvider != nil {
		opts = append(opts, redisotel.WithTracerProvider(conf.TraceProvider))
	}
	opts = append(opts,
		redisotel.WithCallerEnabled(conf.TraceCaller),
		redisotel.WithDBStatement(conf.TraceIncludeCommandArgs),
		redisotel.WithDialFilter(true),
		redisotel.WithCommandFilter(isConnectionManagementCommand),
		redisotel.WithCommandsFilter(isConnectionManagementPipeline),
		redisotel.WithAttributes(attribute.String("micro.redis.name", conf.Name)),
	)
	if len(conf.TraceAttributes) > 0 {
		opts = append(opts, redisotel.WithAttributes(conf.TraceAttributes...))
	}
	return opts
}
