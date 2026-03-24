package gormx

import (
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"gorm.io/gorm"
)

const callbackStartTimeKey = "gormx:start_time"

type observabilityPlugin struct {
	name          string
	logger        *logger.Logger
	enableLogger  bool
	slowThreshold time.Duration
	metrics       *metrics
}

func newObservabilityPlugin(conf *Config, metrics *metrics) gorm.Plugin {
	return &observabilityPlugin{
		name:          conf.Name,
		logger:        conf.Logger,
		enableLogger:  conf.EnableLogger,
		slowThreshold: conf.SlowThreshold,
		metrics:       metrics,
	}
}

func (p *observabilityPlugin) Name() string {
	return "gormx.observability"
}

func (p *observabilityPlugin) Initialize(db *gorm.DB) error {
	return registerCallbacks(db, p.before, map[string]func(*gorm.DB){
		"create": p.after("create"),
		"query":  p.after("query"),
		"update": p.after("update"),
		"delete": p.after("delete"),
		"row":    p.after("row"),
		"raw":    p.after("raw"),
	})
}

func (p *observabilityPlugin) before(db *gorm.DB) {
	db.Statement.Context = normalizeContext(db.Statement.Context)
	db.InstanceSet(callbackStartTimeKey, time.Now())
}

func (p *observabilityPlugin) after(operation string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		ctx := normalizeContext(db.Statement.Context)

		start, ok := db.InstanceGet(callbackStartTimeKey)
		if !ok {
			return
		}
		startTime, ok := start.(time.Time)
		if !ok {
			return
		}

		duration := time.Since(startTime)
		status := queryStatus(db.Error)
		table := tableName(db.Statement)
		query := normalizeSQL(db.Statement.SQL.String())

		if p.metrics != nil {
			p.metrics.dbRequestDuration.WithLabelValues(p.name, operation, status, table).Observe(duration.Seconds())
			p.metrics.dbRequestsTotal.WithLabelValues(p.name, operation, status, table).Inc()
		}

		fields := []any{
			"db", p.name,
			"operation", operation,
			"table", table,
			"status", status,
			"rows", db.RowsAffected,
			"duration", duration,
		}
		if query != "" {
			fields = append(fields, "sql", query)
		}

		switch status {
		case "error":
			fields = append(fields, "error", db.Error)
			p.logger.Error(ctx, "db query failed", fields...)
		case "not_found":
			if p.enableLogger {
				p.logger.Debug(ctx, "db query not found", fields...)
			}
		default:
			if p.enableLogger && p.slowThreshold > 0 && duration >= p.slowThreshold {
				p.logger.Warn(ctx, "db query slow", append(fields, "slow_threshold", p.slowThreshold)...)
				return
			}
			if p.enableLogger {
				p.logger.Debug(ctx, "db query completed", fields...)
			}
		}
	}
}

func registerCallbacks(db *gorm.DB, before func(*gorm.DB), afters map[string]func(*gorm.DB)) error {
	callbacks := db.Callback()

	if err := callbacks.Create().Before("gorm:create").Register("gormx:before_create", before); err != nil {
		return err
	}
	if err := callbacks.Create().After("gorm:create").Register("gormx:after_create", afters["create"]); err != nil {
		return err
	}
	if err := callbacks.Query().Before("gorm:query").Register("gormx:before_query", before); err != nil {
		return err
	}
	if err := callbacks.Query().After("gorm:query").Register("gormx:after_query", afters["query"]); err != nil {
		return err
	}
	if err := callbacks.Update().Before("gorm:update").Register("gormx:before_update", before); err != nil {
		return err
	}
	if err := callbacks.Update().After("gorm:update").Register("gormx:after_update", afters["update"]); err != nil {
		return err
	}
	if err := callbacks.Delete().Before("gorm:delete").Register("gormx:before_delete", before); err != nil {
		return err
	}
	if err := callbacks.Delete().After("gorm:delete").Register("gormx:after_delete", afters["delete"]); err != nil {
		return err
	}
	if err := callbacks.Row().Before("gorm:row").Register("gormx:before_row", before); err != nil {
		return err
	}
	if err := callbacks.Row().After("gorm:row").Register("gormx:after_row", afters["row"]); err != nil {
		return err
	}
	if err := callbacks.Raw().Before("gorm:raw").Register("gormx:before_raw", before); err != nil {
		return err
	}
	if err := callbacks.Raw().After("gorm:raw").Register("gormx:after_raw", afters["raw"]); err != nil {
		return err
	}
	return nil
}
