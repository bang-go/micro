# trace

`trace` 负责把 OpenTelemetry `TracerProvider` 的构造和全局安装边界拆开。它的重点不是隐藏 OTel，而是把默认值、导出器选择和全局副作用做显式。

## 设计原则

- `Open` 只构造 provider，不改全局状态。
- `InitTracer` 明确表达“安装全局 tracer provider”这个动作。
- `Exporter`、`Endpoint`、`SampleRate` 都在包边界完成规范化和校验。
- `Headers` 会被克隆，避免调用方后续修改影响已构建 exporter。
- 公开入口要求非 nil context。

## 快速开始

```go
shutdown, err := trace.InitTracer(context.Background(), &trace.Config{
    ServiceName: "orders",
    Exporter:    trace.ExporterOTLPHTTP,
    Endpoint:    "http://127.0.0.1:4318/v1/traces",
})
if err != nil {
    panic(err)
}
defer shutdown(context.Background())
```

如果你只想拿到 provider，而不安装全局状态：

```go
tp, err := trace.Open(context.Background(), &trace.Config{
    ServiceName: "orders",
    Exporter:    trace.ExporterStdout,
    PrettyPrint: true,
})
if err != nil {
    panic(err)
}
defer tp.Shutdown(context.Background())
```

## API 摘要

```go
type Config struct {
    ServiceName       string
    ServiceInstanceID string
    Endpoint          string
    Exporter          string
    SampleRate        *float64
    Headers           map[string]string
    Compression       string
    Timeout           time.Duration
    Insecure          bool
    PrettyPrint       bool
}

func Open(context.Context, *Config) (*sdktrace.TracerProvider, error)
func InitTracer(context.Context, *Config) (func(context.Context) error, error)
```

## 默认行为

- 默认 `ServiceName` 是 `unknown-service`
- 默认 `ServiceInstanceID` 优先取 `HOSTNAME`，否则取系统主机名
- 默认 exporter 是 `stdout`
- `SampleRate == nil` 时默认全采样
- `Exporter == "otlp"` 时：
  - `Endpoint` 带 `http://` 或 `https://` 前缀时走 OTLP/HTTP
  - 否则走 OTLP/gRPC
