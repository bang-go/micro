package gormx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	otgorm "gorm.io/plugin/opentelemetry/tracing"
)

const (
	DriverMySQL    = "mysql"
	DriverPostgres = "postgres"
	DriverSQLite   = "sqlite"

	defaultPingTimeout   = 5 * time.Second
	defaultSlowThreshold = 500 * time.Millisecond
)

type Config struct {
	Name      string
	Driver    string
	DSN       string
	Dialector gorm.Dialector

	GormConfig      *gorm.Config
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	SkipPing    bool
	PingTimeout time.Duration

	Trace                    bool
	TraceProvider            trace.TracerProvider
	TraceAttributes          []attribute.KeyValue
	TraceIncludeQueryVars    bool
	TraceEnableDBStatsMetric bool
	TraceRecordStackTrace    bool

	Logger            *logger.Logger
	EnableLogger      bool
	SlowThreshold     time.Duration
	DisableMetrics    bool
	MetricsRegisterer prometheus.Registerer
}

type Client interface {
	DB() *gorm.DB
	WithContext(context.Context) *gorm.DB
	SQLDB() *sql.DB
	Ping(context.Context) error
	Stats() sql.DBStats
	Use(gorm.Plugin) error
	Close() error
}

type clientEntity struct {
	db    *gorm.DB
	sqlDB *sql.DB

	closeOnce sync.Once
	closeErr  error
}

func Open(ctx context.Context, conf *Config) (Client, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	config, dialector, gormConfig, err := prepareConfig(conf)
	if err != nil {
		return nil, err
	}

	var metrics *metrics
	if !config.DisableMetrics {
		metrics = defaultGORMMetrics()
		if config.MetricsRegisterer != nil {
			metrics = newGORMMetrics(config.MetricsRegisterer)
		}
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, fmt.Errorf("gormx: open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("gormx: sql db: %w", err)
	}

	configurePool(sqlDB, config)

	cleanup := func(cause error) error {
		closeErr := sqlDB.Close()
		if closeErr != nil {
			return errors.Join(cause, closeErr)
		}
		return cause
	}

	if err := db.Use(newObservabilityPlugin(config, metrics)); err != nil {
		return nil, cleanup(fmt.Errorf("gormx: register observability plugin: %w", err))
	}

	if config.Trace {
		tracePlugin := buildTracePlugin(config)
		if err := db.Use(tracePlugin); err != nil {
			return nil, cleanup(fmt.Errorf("gormx: register trace plugin: %w", err))
		}
	}

	client := &clientEntity{
		db:    db,
		sqlDB: sqlDB,
	}

	if !config.SkipPing {
		pingCtx, cancel := timeoutContext(ctx, config.PingTimeout)
		defer cancel()
		if err := client.Ping(pingCtx); err != nil {
			return nil, cleanup(fmt.Errorf("gormx: ping database: %w", err))
		}
	}

	return client, nil
}

func New(conf *Config) (Client, error) {
	return Open(context.Background(), conf)
}

func (c *clientEntity) DB() *gorm.DB {
	return c.db
}

func (c *clientEntity) WithContext(ctx context.Context) *gorm.DB {
	return c.db.WithContext(normalizeContext(ctx))
}

func (c *clientEntity) SQLDB() *sql.DB {
	return c.sqlDB
}

func (c *clientEntity) Ping(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	return c.sqlDB.PingContext(ctx)
}

func (c *clientEntity) Stats() sql.DBStats {
	return c.sqlDB.Stats()
}

func (c *clientEntity) Use(plugin gorm.Plugin) error {
	if plugin == nil {
		return ErrNilPlugin
	}
	return c.db.Use(plugin)
}

func (c *clientEntity) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.sqlDB.Close()
	})
	return c.closeErr
}

func prepareConfig(conf *Config) (*Config, gorm.Dialector, *gorm.Config, error) {
	if conf == nil {
		return nil, nil, nil, ErrNilConfig
	}

	cloned := *conf
	cloned.Name = strings.TrimSpace(cloned.Name)
	cloned.Driver = strings.ToLower(strings.TrimSpace(cloned.Driver))
	cloned.DSN = strings.TrimSpace(cloned.DSN)
	cloned.TraceAttributes = append([]attribute.KeyValue(nil), cloned.TraceAttributes...)
	cloned.Logger = defaultLogger(cloned.Logger)
	if cloned.PingTimeout == 0 {
		cloned.PingTimeout = defaultPingTimeout
	}
	if cloned.SlowThreshold == 0 {
		cloned.SlowThreshold = defaultSlowThreshold
	}

	dialector, err := buildDialector(&cloned)
	if err != nil {
		return nil, nil, nil, err
	}

	cloned.Name = defaultName(cloned.Name, cloned.Driver, dialector)

	gormConfig := cloneGORMConfig(cloned.GormConfig)
	gormConfig.Logger = defaultGORMLogger(gormConfig.Logger)

	return &cloned, dialector, gormConfig, nil
}

func buildDialector(conf *Config) (gorm.Dialector, error) {
	if conf.Dialector != nil {
		return conf.Dialector, nil
	}
	if conf.Driver == "" {
		return nil, ErrDriverRequired
	}
	if conf.DSN == "" {
		return nil, ErrDSNRequired
	}

	switch conf.Driver {
	case DriverMySQL:
		return mysql.Open(conf.DSN), nil
	case DriverPostgres:
		return postgres.Open(conf.DSN), nil
	case DriverSQLite:
		return sqlite.Open(conf.DSN), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDriver, conf.Driver)
	}
}

func configurePool(sqlDB *sql.DB, conf *Config) {
	if conf.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(conf.MaxIdleConns)
	}
	if conf.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(conf.MaxOpenConns)
	}
	if conf.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(conf.ConnMaxLifetime)
	}
	if conf.ConnMaxIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(conf.ConnMaxIdleTime)
	}
}

func buildTracePlugin(conf *Config) gorm.Plugin {
	opts := make([]otgorm.Option, 0, 5)
	if conf.TraceProvider != nil {
		opts = append(opts, otgorm.WithTracerProvider(conf.TraceProvider))
	}
	opts = append(opts, otgorm.WithQueryFormatter(normalizeSQL))
	opts = append(opts, otgorm.WithAttributes(tracingAttributes(conf.Name, conf.TraceAttributes)...))
	if !conf.TraceIncludeQueryVars {
		opts = append(opts, otgorm.WithoutQueryVariables())
	}
	if !conf.TraceEnableDBStatsMetric {
		opts = append(opts, otgorm.WithoutMetrics())
	}
	if conf.TraceRecordStackTrace {
		opts = append(opts, otgorm.WithRecordStackTrace())
	}
	return otgorm.NewPlugin(opts...)
}
