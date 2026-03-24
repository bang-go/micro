# grpcx

`grpcx` 是基于 `google.golang.org/grpc` 的生产级封装，提供统一的拦截器管理和最佳实践配置。

## ✨ 特性

*   **双向拦截器**：客户端和服务端均集成 Recovery, Metrics, Logger, Tracing 拦截器。
*   **Panic 恢复**：自动捕获 Panic 并打印堆栈，防止服务崩溃。
*   **可观测性**：无缝集成 OpenTelemetry 和 Prometheus。
*   **服务生命周期**：`Start(ctx)` 与 `ctx` 绑定，取消 `ctx` 会优雅关闭服务。
*   **保守默认值**：未显式配置 KeepAlive 时，保持 `grpc-go` 官方默认行为，不擅自注入激进策略。

## 🚀 快速开始

### 服务端 (Server)

```go
import (
    "context"
    "github.com/bang-go/micro/transport/grpcx"
    "google.golang.org/grpc"
)

func main() {
    // 1. 创建 Server
    srv := grpcx.NewServer(&grpcx.ServerConfig{
        Addr:         ":9090",
        Trace:        true,
        EnableLogger: true,
    })

    // 2. 启动并注册服务
    err := srv.Start(context.Background(), func(s *grpc.Server) {
        // pb.RegisterGreeterServer(s, &server{})
    })
    if err != nil {
        panic(err)
    }
}
```

### 客户端 (Client)

```go
import (
    "context"
    "github.com/bang-go/micro/transport/grpcx"
)

func main() {
    // 1. 创建 Client
    cli := grpcx.NewClient(&grpcx.ClientConfig{
        Addr:         "localhost:9090",
        Trace:        true,
        EnableLogger: true,
    })

    // 2. 获取连接并等待 Ready
    conn, err := cli.DialContext(context.Background())
    if err != nil {
        panic(err)
    }
    defer conn.Close()

    // 3. 调用 RPC
    // client := pb.NewGreeterClient(conn)
    // resp, err := client.SayHello(ctx, req)
}
```

## ⚙️ 配置说明

### ServerConfig

```go
type ServerConfig struct {
    Addr         string
    Listener     net.Listener // 可选，便于测试或嵌入已有 listener
    KeepaliveEnforcementPolicy *keepalive.EnforcementPolicy
    KeepaliveParams            *keepalive.ServerParameters
    MetricsRegisterer          prometheus.Registerer // 可选，默认使用 prometheus.DefaultRegisterer
    DisableMetrics             bool
    Trace        bool
    Logger       *logger.Logger
    EnableLogger bool
}
```

### ClientConfig

```go
type ClientConfig struct {
    Addr                 string
    Secure               bool // 为 true 且未提供自定义凭证时，默认启用 TLS1.2+
    TLSConfig            *tls.Config
    TransportCredentials credentials.TransportCredentials
    KeepaliveParams      *keepalive.ClientParameters
    MetricsRegisterer    prometheus.Registerer // 可选，默认使用 prometheus.DefaultRegisterer
    DisableMetrics       bool
    Trace                bool
    Logger               *logger.Logger
    EnableLogger         bool
}
```

## 设计说明

*   `Start(ctx, ...)` 中的 `ctx` 是真正的服务生命周期控制，不只是日志透传。
*   Metrics 默认注册到 `prometheus.DefaultRegisterer`；如果你需要隔离 registry，可通过 `MetricsRegisterer` 注入。
*   `metadatax` 遵循 `grpc-go/metadata` 原生语义，`-bin` value 保持原始值，由 gRPC 传输层负责编码。
