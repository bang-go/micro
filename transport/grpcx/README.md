# grpcx

`grpcx` 是基于 `google.golang.org/grpc` 的生产级封装，提供统一的拦截器管理和最佳实践配置。

## ✨ 特性

*   **双向拦截器**：客户端和服务端均集成 Recovery, Metrics, Logger, Tracing 拦截器。
*   **Panic 恢复**：自动捕获 Panic 并打印堆栈，防止服务崩溃。
*   **可观测性**：无缝集成 OpenTelemetry 和 Prometheus。
*   **开箱即用**：预设合理的 KeepAlive 和超时参数。

## 🚀 快速开始

### 服务端 (Server)

```go
import (
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
    err := srv.Start(func(s *grpc.Server) {
        // pb.RegisterGreeterServer(s, &server{})
    })
    if err != nil {
        panic(err)
    }
}
```

### 客户端 (Client)

```go
import "github.com/bang-go/micro/transport/grpcx"

func main() {
    // 1. 创建 Client
    cli := grpcx.NewClient(&grpcx.ClientConfig{
        Addr:         "localhost:9090",
        Trace:        true,
        EnableLogger: true,
    })

    // 2. 获取连接
    conn, err := cli.Dial()
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
    Trace        bool
    Logger       *logger.Logger
    EnableLogger bool
}
```

### ClientConfig

```go
type ClientConfig struct {
    Addr         string
    Secure       bool // 是否启用 TLS
    Trace        bool
    Logger       *logger.Logger
    EnableLogger bool
}
```
