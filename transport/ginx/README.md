# ginx

`ginx` 是对 `gin-gonic/gin` 的生产级封装。它保留 Gin 的路由能力，同时补齐服务端生命周期、可观测性、panic recovery 与健康检查边界。

## 设计原则

- 不在 `init()` 中注册 Prometheus 指标，指标统一按需懒注册。
- metrics 支持注入自定义 `prometheus.Registerer`，而不是强绑全局 registry。
- 不把健康检查直接注册进 Gin 路由表，避免和业务路由冲突。
- 默认日志不记录 query string，避免把 token、签名等敏感信息写进访问日志。
- 指标按路由模板聚合，未命中路由统一标记为 `unmatched`，避免高基数标签。
- 服务端具备显式 `Start` / `Serve` / `Shutdown` / `Close` 生命周期和运行态保护，且请求上下文会继承服务上下文。

## 🚀 快速开始

```go
package main

import (
    "context"
    "github.com/bang-go/micro/transport/ginx"
    "github.com/gin-gonic/gin"
)

func main() {
    srv := ginx.New(&ginx.ServerConfig{
        Addr:         ":8080",
        Trace:        true,
        EnableLogger: true,
    })

    r := srv.GinEngine()
    r.GET("/hello", func(c *gin.Context) {
        c.JSON(200, gin.H{"msg": "world"})
    })

    if err := srv.Start(context.Background()); err != nil {
        panic(err)
    }
}
```

## 默认行为

- 自动暴露健康检查 `GET /healthz`
- 框架托管的健康检查路径、`/metrics`、`/favicon.ico` 默认跳过 trace / metrics / access log
- panic recovery 会记录结构化日志和堆栈
- `Shutdown` 在没有 deadline 时会自动使用 `ShutdownTimeout`
- `Start` / `Serve` / `Shutdown` 要求显式传入非 `nil` context

如果你不希望框架接管健康检查：

```go
srv := ginx.New(&ginx.ServerConfig{
    Addr:                  ":8080",
    DisableHealthEndpoint: true,
})
```

## 核心接口

```go
type Server interface {
    Start(context.Context) error
    Serve(context.Context, net.Listener) error
    Shutdown(context.Context) error
    Close() error
    HTTPServer() *http.Server
    GinEngine() *gin.Engine
}
```

## 配置补充

```go
type ServerConfig struct {
    MetricsRegisterer prometheus.Registerer // 可选，默认使用 prometheus.DefaultRegisterer
    DisableMetrics    bool
}
```

## 行为边界

- 只有框架托管的健康检查路径会被默认跳过观测性；如果你关闭健康检查并自行注册同路径业务路由，该路由不会再被静默跳过。
- `http.Server.BaseContext` 会绑定到 `Start/Serve` 的 `ctx`，请求上下文可直接继承服务级 value / cancellation 语义。
