package gormx

import (
	"errors"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"
)

var (
	DBRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "micro_db_request_duration_seconds",
			Help:    "Database request duration in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"db_name", "operation", "status", "table"},
	)

	DBRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "micro_db_requests_total",
			Help: "Database requests total",
		},
		[]string{"db_name", "operation", "status", "table"},
	)
)

func init() {
	prometheus.MustRegister(DBRequestDuration)
	prometheus.MustRegister(DBRequestsTotal)
}

type Config struct {
	Name         string // Database logical name for metrics (e.g., "users_db")
	Driver       string // "mysql" or "postgres"
	DSN          string
	MaxIdleConns int
	MaxOpenConns int
	MaxLifetime  time.Duration
	Trace        bool

	Logger       *logger.Logger
	EnableLogger bool
}

func New(conf *Config) (*gorm.DB, error) {
	if conf == nil {
		return nil, errors.New("config is nil")
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}
	if conf.Name == "" {
		conf.Name = "default"
	}

	var dialector gorm.Dialector
	switch conf.Driver {
	case "mysql":
		dialector = mysql.Open(conf.DSN)
	case "postgres":
		dialector = postgres.Open(conf.DSN)
	default:
		return nil, errors.New("unsupported driver: " + conf.Driver)
	}

	// Disable default logger to avoid duplicate logs, or set to silent
	// We will use our own plugin for logging
	gormConfig := &gorm.Config{
		Logger: gormlogger.Discard,
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	if conf.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(conf.MaxIdleConns)
	}
	if conf.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(conf.MaxOpenConns)
	}
	if conf.MaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(conf.MaxLifetime)
	}

	// Register Plugin
	err = db.Use(&plugin{
		logger:       conf.Logger,
		enableLogger: conf.EnableLogger,
		dbName:       conf.Name,
	})
	if err != nil {
		return nil, err
	}

	if conf.Trace {
		if err := db.Use(tracing.NewPlugin()); err != nil {
			return nil, err
		}
	}

	return db, nil
}

type plugin struct {
	logger       *logger.Logger
	enableLogger bool
	dbName       string
}

func (p *plugin) Name() string {
	return "micro_gorm_plugin"
}

func (p *plugin) Initialize(db *gorm.DB) error {
	// Register callbacks
	return p.registerCallbacks(db)
}

func (p *plugin) registerCallbacks(db *gorm.DB) error {
	// Create
	if err := db.Callback().Create().Before("gorm:create").Register("micro:before_create", p.before); err != nil {
		return err
	}
	if err := db.Callback().Create().After("gorm:create").Register("micro:after_create", p.after("create")); err != nil {
		return err
	}

	// Query
	if err := db.Callback().Query().Before("gorm:query").Register("micro:before_query", p.before); err != nil {
		return err
	}
	if err := db.Callback().Query().After("gorm:query").Register("micro:after_query", p.after("query")); err != nil {
		return err
	}

	// Update
	if err := db.Callback().Update().Before("gorm:update").Register("micro:before_update", p.before); err != nil {
		return err
	}
	if err := db.Callback().Update().After("gorm:update").Register("micro:after_update", p.after("update")); err != nil {
		return err
	}

	// Delete
	if err := db.Callback().Delete().Before("gorm:delete").Register("micro:before_delete", p.before); err != nil {
		return err
	}
	if err := db.Callback().Delete().After("gorm:delete").Register("micro:after_delete", p.after("delete")); err != nil {
		return err
	}

	// Row
	if err := db.Callback().Row().Before("gorm:row").Register("micro:before_row", p.before); err != nil {
		return err
	}
	if err := db.Callback().Row().After("gorm:row").Register("micro:after_row", p.after("row")); err != nil {
		return err
	}

	// Raw
	if err := db.Callback().Raw().Before("gorm:raw").Register("micro:before_raw", p.before); err != nil {
		return err
	}
	if err := db.Callback().Raw().After("gorm:raw").Register("micro:after_raw", p.after("raw")); err != nil {
		return err
	}

	return nil
}

const startTimeKey = "micro:start_time"

func (p *plugin) before(db *gorm.DB) {
	db.InstanceSet(startTimeKey, time.Now())
}

func (p *plugin) after(operation string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		startTime, ok := db.InstanceGet(startTimeKey)
		if !ok {
			return
		}

		t, ok := startTime.(time.Time)
		if !ok {
			return
		}

		duration := time.Since(t).Seconds()
		status := "success"
		if db.Error != nil && !errors.Is(db.Error, gorm.ErrRecordNotFound) {
			status = "error"
		}

		table := db.Statement.Table
		if table == "" {
			table = "unknown"
		}

		// Metrics
		DBRequestDuration.WithLabelValues(p.dbName, operation, status, table).Observe(duration)
		DBRequestsTotal.WithLabelValues(p.dbName, operation, status, table).Inc()

		// Logging
		if p.enableLogger {
			sql := db.Dialector.Explain(db.Statement.SQL.String(), db.Statement.Vars...)

			if status == "error" {
				p.logger.Error(db.Statement.Context, "db_query_failed",
					"db", p.dbName,
					"operation", operation,
					"table", table,
					"sql", sql,
					"rows", db.RowsAffected,
					"error", db.Error,
					"cost", duration,
				)
			} else {
				p.logger.Info(db.Statement.Context, "db_access_log",
					"db", p.dbName,
					"operation", operation,
					"table", table,
					"sql", sql,
					"rows", db.RowsAffected,
					"status", status,
					"cost", duration,
				)
			}
		}
	}
}
