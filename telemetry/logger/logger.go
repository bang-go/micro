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

// Logger is a wrapper around slog.Logger with toggle capability and caller fix.
type Logger struct {
	handler slog.Handler
	enabled *atomic.Bool // Pointer to allow sharing state across derived loggers
}

type options struct {
	level     slog.Level
	format    string // "json", "text"
	addSource bool
	output    io.Writer
}

// Option configures the Logger
type Option func(*options)

func WithLevel(level string) Option {
	return func(o *options) {
		switch strings.ToLower(level) {
		case "debug":
			o.level = slog.LevelDebug
		case "warn":
			o.level = slog.LevelWarn
		case "error":
			o.level = slog.LevelError
		default:
			o.level = slog.LevelInfo
		}
	}
}

func WithFormat(format string) Option {
	return func(o *options) {
		o.format = strings.ToLower(format)
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

// New creates a new Logger instance.
func New(opts ...Option) *Logger {
	config := &options{
		level:     slog.LevelInfo,
		format:    "json",
		addSource: true, // Default to true for enterprise debugging
		output:    os.Stdout,
	}
	for _, opt := range opts {
		opt(config)
	}

	hOpts := &slog.HandlerOptions{
		Level:     config.level,
		AddSource: config.addSource,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				t := a.Value.Time()
				a.Value = slog.StringValue(t.Format(time.RFC3339))
			}
			if a.Key == slog.SourceKey {
				source, ok := a.Value.Any().(*slog.Source)
				if ok {
					// Shorten file path
					source.File = filepath.Base(source.File)
				}
			}
			return a
		},
	}

	var handler slog.Handler
	if config.format == "text" {
		handler = slog.NewTextHandler(config.output, hOpts)
	} else {
		handler = slog.NewJSONHandler(config.output, hOpts)
	}

	val := atomic.Bool{}
	val.Store(true) // Default enabled

	return &Logger{
		handler: handler,
		enabled: &val,
	}
}

// Toggle enables or disables log output
func (l *Logger) Toggle(enable bool) {
	l.enabled.Store(enable)
}

// IsEnabled checks if logging is enabled
func (l *Logger) IsEnabled() bool {
	return l.enabled.Load()
}

// Info logs at LevelInfo with context (auto trace_id injection).
func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelInfo, msg, args...)
}

// Debug logs at LevelDebug with context (auto trace_id injection).
func (l *Logger) Debug(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelDebug, msg, args...)
}

// Warn logs at LevelWarn with context (auto trace_id injection).
func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelWarn, msg, args...)
}

// Error logs at LevelError with context (auto trace_id injection).
func (l *Logger) Error(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelError, msg, args...)
}

// With returns a new Logger with the given attributes.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		handler: l.handler.WithAttrs(argsToAttrs(args)),
		enabled: l.enabled, // Share the enabled switch
	}
}

// WithGroup returns a new Logger that starts a group.
func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{
		handler: l.handler.WithGroup(name),
		enabled: l.enabled,
	}
}

// Internal helper to handle logging with correct caller depth
func (l *Logger) log(ctx context.Context, level slog.Level, msg string, args ...any) {
	if !l.enabled.Load() {
		return
	}
	if !l.handler.Enabled(ctx, level) {
		return
	}

	var pcs [1]uintptr
	// skip [runtime.Callers, this function, wrapper function (Info/Debug/etc)]
	runtime.Callers(3, pcs[:])

	r := slog.NewRecord(time.Now(), level, msg, pcs[0])

	// Inject TraceID and SpanID from context if available
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		args = append(args, "trace_id", span.SpanContext().TraceID().String())
		args = append(args, "span_id", span.SpanContext().SpanID().String())
	}

	r.Add(args...)
	_ = l.handler.Handle(ctx, r)
}

// Helper to convert args to slog.Attr (simplified from slog internals)
func argsToAttrs(args []any) []slog.Attr {
	var attrs []slog.Attr
	for len(args) > 0 {
		switch x := args[0].(type) {
		case string:
			if len(args) == 1 {
				attrs = append(attrs, slog.String("!BADKEY", x))
				args = args[1:]
			} else {
				attrs = append(attrs, slog.Any(x, args[1]))
				args = args[2:]
			}
		case slog.Attr:
			attrs = append(attrs, x)
			args = args[1:]
		default:
			attrs = append(attrs, slog.Any("!BADKEY", x))
			args = args[1:]
		}
	}
	return attrs
}

// Compatibility with bang-go/opt if needed, or just helpers
func (l *Logger) GetSlog() *slog.Logger {
	return slog.New(l.handler)
}
