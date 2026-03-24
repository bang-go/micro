package gormx

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var sqlWhitespacePattern = regexp.MustCompile(`\s+`)

func normalizeContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func cloneGORMConfig(cfg *gorm.Config) *gorm.Config {
	if cfg == nil {
		return &gorm.Config{}
	}
	cloned := *cfg
	return &cloned
}

func defaultGORMLogger(existing gormlogger.Interface) gormlogger.Interface {
	if existing != nil {
		return existing
	}
	return gormlogger.Default.LogMode(gormlogger.Silent)
}

func defaultLogger(log *logger.Logger) *logger.Logger {
	if log != nil {
		return log
	}
	return logger.New(logger.WithLevel("info"))
}

func defaultName(name, driver string, dialector gorm.Dialector) string {
	if name != "" {
		return name
	}
	if driver != "" {
		return driver
	}
	if dialector != nil && dialector.Name() != "" {
		return dialector.Name()
	}
	return "default"
}

func normalizeSQL(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	return sqlWhitespacePattern.ReplaceAllString(query, " ")
}

func tableName(statement *gorm.Statement) string {
	if statement == nil {
		return "unknown"
	}
	switch {
	case statement.Table != "":
		return statement.Table
	case statement.Schema != nil && statement.Schema.Table != "":
		return statement.Schema.Table
	default:
		return "unknown"
	}
}

func queryStatus(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, sql.ErrNoRows):
		return "not_found"
	default:
		return "error"
	}
}

func tracingAttributes(name string, attrs []attribute.KeyValue) []attribute.KeyValue {
	all := make([]attribute.KeyValue, 0, len(attrs)+1)
	all = append(all, attribute.String("micro.db.name", name))
	all = append(all, attrs...)
	return all
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
