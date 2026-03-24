package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type Logger struct {
	handler slog.Handler
	level   *slog.LevelVar
	enabled *atomic.Bool
}

type contextHandler struct {
	base    slog.Handler
	enabled *atomic.Bool
}

type options struct {
	level     slog.Level
	format    string
	addSource bool
	output    io.Writer
}

type Option func(*options)

func WithLevel(level string) Option {
	return func(o *options) {
		o.level = parseLevel(level)
	}
}

func WithFormat(format string) Option {
	return func(o *options) {
		o.format = strings.ToLower(strings.TrimSpace(format))
	}
}

func WithAddSource(add bool) Option {
	return func(o *options) {
		o.addSource = add
	}
}

func WithOutput(w io.Writer) Option {
	return func(o *options) {
		o.output = w
	}
}

func New(opts ...Option) *Logger {
	config := &options{
		level:     slog.LevelInfo,
		format:    "json",
		addSource: true,
		output:    os.Stdout,
	}
	for _, opt := range opts {
		opt(config)
	}
	if config.output == nil {
		config.output = io.Discard
	}

	levelVar := &slog.LevelVar{}
	levelVar.Set(config.level)

	handlerOptions := &slog.HandlerOptions{
		Level:     levelVar,
		AddSource: config.addSource,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case slog.TimeKey:
				if timestamp := attr.Value.Time(); !timestamp.IsZero() {
					attr.Value = slog.StringValue(timestamp.Format(time.RFC3339))
				}
			case slog.SourceKey:
				if source, ok := attr.Value.Any().(*slog.Source); ok && source != nil {
					source.File = filepath.Base(source.File)
				}
			}
			return attr
		},
	}

	var handler slog.Handler
	switch config.format {
	case "", "json":
		handler = slog.NewJSONHandler(config.output, handlerOptions)
	default:
		handler = slog.NewTextHandler(config.output, handlerOptions)
	}

	enabled := &atomic.Bool{}
	enabled.Store(true)
	handler = &contextHandler{
		base:    handler,
		enabled: enabled,
	}

	return &Logger{
		handler: handler,
		level:   levelVar,
		enabled: enabled,
	}
}

func (l *Logger) Toggle(enable bool) {
	l.enabled.Store(enable)
}

func (l *Logger) IsEnabled() bool {
	return l.enabled.Load()
}

func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelInfo, msg, args...)
}

func (l *Logger) Debug(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelDebug, msg, args...)
}

func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelWarn, msg, args...)
}

func (l *Logger) Error(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelError, msg, args...)
}

func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		handler: l.handler.WithAttrs(argsToAttrs(args)),
		level:   l.level,
		enabled: l.enabled,
	}
}

func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{
		handler: l.handler.WithGroup(name),
		level:   l.level,
		enabled: l.enabled,
	}
}

func (l *Logger) GetSlog() *slog.Logger {
	return slog.New(l.handler)
}

func (l *Logger) log(ctx context.Context, level slog.Level, msg string, args ...any) {
	if l == nil || !l.enabled.Load() {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !l.handler.Enabled(ctx, level) {
		return
	}

	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	record := slog.NewRecord(time.Now(), level, msg, pcs[0])

	record.AddAttrs(argsToAttrs(args)...)
	_ = l.handler.Handle(ctx, record)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func argsToAttrs(args []any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(args))
	for len(args) > 0 {
		switch value := args[0].(type) {
		case slog.Attr:
			attrs = append(attrs, value)
			args = args[1:]
		case string:
			if len(args) == 1 {
				attrs = append(attrs, slog.String("!BADKEY", value))
				args = args[1:]
				continue
			}
			attrs = append(attrs, slog.Any(value, args[1]))
			args = args[2:]
		default:
			attrs = append(attrs, slog.Any("!BADKEY", value))
			args = args[1:]
		}
	}
	return attrs
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h == nil || h.base == nil || h.enabled == nil || !h.enabled.Load() {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return h.base.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, record slog.Record) error {
	if h == nil || h.base == nil || h.enabled == nil || !h.enabled.Load() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		updated := record.Clone()
		if !recordHasAttr(record, "trace_id") {
			updated.AddAttrs(slog.String("trace_id", spanContext.TraceID().String()))
		}
		if !recordHasAttr(record, "span_id") {
			updated.AddAttrs(slog.String("span_id", spanContext.SpanID().String()))
		}
		record = updated
	}

	return h.base.Handle(ctx, record)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{
		base:    h.base.WithAttrs(attrs),
		enabled: h.enabled,
	}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{
		base:    h.base.WithGroup(name),
		enabled: h.enabled,
	}
}

func recordHasAttr(record slog.Record, key string) bool {
	found := false
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == key {
			found = true
			return false
		}
		return true
	})
	return found
}
