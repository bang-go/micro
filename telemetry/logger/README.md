# logger

`logger` 是基于 `log/slog` 的轻量封装。它不试图接管全局日志体系，只提供统一格式、trace 上下文字段注入和运行时开关。

## 设计原则

- 默认输出 JSON，适合直接进采集链路。
- source 字段只保留文件名，避免冗长绝对路径污染日志面。
- `trace_id` / `span_id` 仅在上下文里有有效 span 时注入。
- 不强制要求非 nil context；日志在没有上下文时仍可安全输出。
- `With` / `WithGroup` 共享启用状态和级别状态，便于统一开关。

## 快速开始

```go
log := logger.New(
    logger.WithLevel("debug"),
    logger.WithFormat("json"),
    logger.WithAddSource(true),
)

log.Info(context.Background(), "service started", "service", "api")

httpLog := log.With("service", "api").WithGroup("http")
httpLog.Debug(context.Background(), "request completed", "status", 200)

slogLogger := log.GetSlog()
slogLogger.Info("plain slog is still available")
```

## API 摘要

```go
func New(opts ...Option) *Logger
func WithLevel(string) Option
func WithFormat(string) Option
func WithAddSource(bool) Option
func WithOutput(io.Writer) Option

func (l *Logger) Toggle(bool)
func (l *Logger) IsEnabled() bool
func (l *Logger) Debug(context.Context, string, ...any)
func (l *Logger) Info(context.Context, string, ...any)
func (l *Logger) Warn(context.Context, string, ...any)
func (l *Logger) Error(context.Context, string, ...any)
func (l *Logger) With(...any) *Logger
func (l *Logger) WithGroup(string) *Logger
func (l *Logger) GetSlog() *slog.Logger
```

## 默认行为

- 默认级别是 `info`
- 默认格式是 `json`
- 默认开启 `source`
- 传入未知级别会回落到 `info`
- 传入未知格式会回落到 text handler
