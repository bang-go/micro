package gormx_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bang-go/micro/store/gormx"
	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type testUser struct {
	ID    uint   `gorm:"primaryKey"`
	Email string `gorm:"uniqueIndex"`
	Name  string
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestOpenValidation(t *testing.T) {
	_, err := gormx.Open(nil, &gormx.Config{
		Driver: gormx.DriverSQLite,
		DSN:    "file::memory:?cache=shared",
	})
	if !errors.Is(err, gormx.ErrContextRequired) {
		t.Fatalf("Open(nil, ...) error = %v, want %v", err, gormx.ErrContextRequired)
	}

	_, err = gormx.Open(context.Background(), nil)
	if !errors.Is(err, gormx.ErrNilConfig) {
		t.Fatalf("Open(nil) error = %v, want %v", err, gormx.ErrNilConfig)
	}

	_, err = gormx.New(&gormx.Config{})
	if !errors.Is(err, gormx.ErrDriverRequired) {
		t.Fatalf("missing driver error = %v, want %v", err, gormx.ErrDriverRequired)
	}

	_, err = gormx.New(&gormx.Config{Driver: gormx.DriverSQLite})
	if !errors.Is(err, gormx.ErrDSNRequired) {
		t.Fatalf("missing dsn error = %v, want %v", err, gormx.ErrDSNRequired)
	}

	_, err = gormx.New(&gormx.Config{Driver: "oracle", DSN: "db"})
	if !errors.Is(err, gormx.ErrUnsupportedDriver) {
		t.Fatalf("unsupported driver error = %v, want %v", err, gormx.ErrUnsupportedDriver)
	}
}

func TestDriverAndDSNAreNormalized(t *testing.T) {
	client, err := gormx.New(&gormx.Config{
		Driver:   " SQLITE ",
		DSN:      " file::memory:?cache=shared ",
		SkipPing: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()
	if client.DB() == nil {
		t.Fatal("DB() returned nil")
	}
}

func TestClientLifecycleAndCRUDWithSQLite(t *testing.T) {
	client, err := gormx.Open(context.Background(), &gormx.Config{
		Name:            "sqlite-test",
		Driver:          gormx.DriverSQLite,
		DSN:             "file::memory:?cache=shared",
		MaxIdleConns:    2,
		MaxOpenConns:    4,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer client.Close()

	if client.DB() == nil {
		t.Fatal("DB() returned nil")
	}
	if client.SQLDB() == nil {
		t.Fatal("SQLDB() returned nil")
	}

	db := client.WithContext(context.Background())
	if err := db.AutoMigrate(&testUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := &testUser{Email: "alice@example.com", Name: "Alice"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var loaded testUser
	if err := db.Where("email = ?", "alice@example.com").First(&loaded).Error; err != nil {
		t.Fatalf("first: %v", err)
	}
	if got, want := loaded.Name, "Alice"; got != want {
		t.Fatalf("loaded.Name = %q, want %q", got, want)
	}

	if err := db.Model(&loaded).Update("name", "Alice 2").Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := db.Delete(&loaded).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}

	if err := db.Exec("SELECT 1").Error; err != nil {
		t.Fatalf("raw exec: %v", err)
	}

	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := client.Ping(nil); !errors.Is(err, gormx.ErrContextRequired) {
		t.Fatalf("Ping(nil) error = %v, want %v", err, gormx.ErrContextRequired)
	}
	stats := client.Stats()
	if got, want := stats.MaxOpenConnections, 4; got != want {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, want)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close idempotent: %v", err)
	}
}

func TestClientUsePlugin(t *testing.T) {
	client, err := gormx.New(&gormx.Config{
		Driver:   gormx.DriverSQLite,
		DSN:      "file::memory:?cache=shared",
		SkipPing: true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer client.Close()

	if err := client.Use(nil); !errors.Is(err, gormx.ErrNilPlugin) {
		t.Fatalf("Use(nil) error = %v, want %v", err, gormx.ErrNilPlugin)
	}

	plugin := &spyPlugin{}
	if err := client.Use(plugin); err != nil {
		t.Fatalf("Use(spyPlugin): %v", err)
	}
	if !plugin.initialized {
		t.Fatal("spy plugin was not initialized")
	}
}

func TestLoggerDoesNotLeakQueryValues(t *testing.T) {
	var logs safeBuffer
	client, err := gormx.New(&gormx.Config{
		Name:         "log-test",
		Driver:       gormx.DriverSQLite,
		DSN:          "file::memory:?cache=shared",
		EnableLogger: true,
		Logger:       loggerForTest(&logs),
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer client.Close()

	db := client.WithContext(context.Background())
	if err := db.AutoMigrate(&testUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := &testUser{Email: "secret@example.com", Name: "Secret"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Create(user).Error; err == nil {
		t.Fatal("expected duplicate create to fail")
	}

	output := logs.String()
	if strings.Contains(output, "secret@example.com") {
		t.Fatalf("logs leaked query value: %q", output)
	}
	//if !strings.Contains(output, "INSERT INTO `test_users`") && !strings.Contains(output, "INSERT INTO \"test_users\"") {
	//	t.Fatalf("logs missing SQL template: %q", output)
	//}
}

func TestTracePluginCreatesSpanWithoutQueryVariables(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider()
	provider.RegisterSpanProcessor(recorder)
	defer provider.Shutdown(context.Background())

	client, err := gormx.New(&gormx.Config{
		Name:          "trace-test",
		Driver:        gormx.DriverSQLite,
		DSN:           "file::memory:?cache=shared",
		Trace:         true,
		TraceProvider: provider,
		TraceAttributes: []attribute.KeyValue{
			attribute.String("component", "gormx-test"),
		},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer client.Close()

	db := client.WithContext(context.Background())
	if err := db.AutoMigrate(&testUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&testUser{Email: "trace@example.com", Name: "Trace"}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var user testUser
	if err := db.Where("email = ?", "trace@example.com").First(&user).Error; err != nil {
		t.Fatalf("query: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}

	var queryText string
	for _, span := range spans {
		for _, attr := range span.Attributes() {
			if string(attr.Key) == "db.query.text" {
				queryText = attr.Value.AsString()
			}
		}
	}
	if queryText == "" {
		t.Fatal("db.query.text attribute missing")
	}
	if strings.Contains(queryText, "trace@example.com") {
		t.Fatalf("span leaked query variable: %q", queryText)
	}
}

func TestNewCanBeCalledRepeatedly(t *testing.T) {
	first, err := gormx.New(&gormx.Config{
		Driver:   gormx.DriverSQLite,
		DSN:      "file::memory:?cache=shared",
		SkipPing: true,
	})
	if err != nil {
		t.Fatalf("first new: %v", err)
	}
	defer first.Close()

	second, err := gormx.New(&gormx.Config{
		Dialector: sqlite.Open("file::memory:?cache=shared"),
		SkipPing:  true,
	})
	if err != nil {
		t.Fatalf("second new: %v", err)
	}
	defer second.Close()
}

func TestMetricsRegistererAndDisableMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	client, err := gormx.New(&gormx.Config{
		Driver:            gormx.DriverSQLite,
		DSN:               "file::memory:?cache=shared",
		SkipPing:          true,
		MetricsRegisterer: reg,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer client.Close()

	db := client.WithContext(context.Background())
	if err := db.AutoMigrate(&testUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&testUser{Email: "metrics@example.com", Name: "Metrics"}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	var foundDuration bool
	var foundTotal bool
	for _, family := range families {
		switch family.GetName() {
		case "gormx_request_duration_seconds":
			foundDuration = true
		case "gormx_requests_total":
			foundTotal = true
		}
	}
	if !foundDuration || !foundTotal {
		t.Fatalf("gormx metrics not registered: duration=%v total=%v", foundDuration, foundTotal)
	}

	disabledReg := prometheus.NewRegistry()
	disabledClient, err := gormx.New(&gormx.Config{
		Driver:            gormx.DriverSQLite,
		DSN:               "file::memory:?cache=shared",
		SkipPing:          true,
		DisableMetrics:    true,
		MetricsRegisterer: disabledReg,
	})
	if err != nil {
		t.Fatalf("new(disable metrics): %v", err)
	}
	defer disabledClient.Close()

	db = disabledClient.WithContext(context.Background())
	if err := db.AutoMigrate(&testUser{}); err != nil {
		t.Fatalf("migrate(disable metrics): %v", err)
	}
	if err := db.Create(&testUser{Email: "disabled@example.com", Name: "Disabled"}).Error; err != nil {
		t.Fatalf("create(disable metrics): %v", err)
	}

	families, err = disabledReg.Gather()
	if err != nil {
		t.Fatalf("gather disabled metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() == "gormx_request_duration_seconds" || family.GetName() == "gormx_requests_total" {
			t.Fatalf("disabled metrics should not be registered, got %s", family.GetName())
		}
	}
}

type spyPlugin struct {
	initialized bool
}

func (p *spyPlugin) Name() string {
	return "gormx.spy"
}

func (p *spyPlugin) Initialize(*gorm.DB) error {
	p.initialized = true
	return nil
}

func loggerForTest(output *safeBuffer) *logger.Logger {
	return logger.New(
		logger.WithFormat("text"),
		logger.WithAddSource(false),
		logger.WithLevel("debug"),
		logger.WithOutput(output),
	)
}
