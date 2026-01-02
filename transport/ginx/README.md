# ginx

`ginx` 是基于 `gin-gonic/gin` 的微服务 Web 框架封装，集成了生产级中间件和最佳实践。

## ✨ 特性

*   **全链路追踪**：内置 OpenTelemetry 中间件，自动串联 TraceID。
*   **统一日志**：集成结构化日志中间件，记录请求耗时、状态码、IP 等。
*   **监控指标**：内置 Prometheus Metrics 中间件，自动暴露 HTTP RED 指标。
*   **Panic 恢复**：增强型 Recovery 中间件，发生 Panic 时自动记录堆栈日志并返回 500。
*   **平滑关闭**：支持优雅停机 (Graceful Shutdown)。

## 🚀 快速开始

```go
package main

import (
    "github.com/bang-go/micro/transport/ginx"
    "github.com/gin-gonic/gin"
)

func main() {
    // 1. 初始化 Server
    srv := ginx.New(&ginx.ServerConfig{
        Addr:         ":8080",
        Trace:        true, // 开启追踪
        EnableLogger: true, // 开启日志
    })

    // 2. 获取原生 Gin Engine 进行路由注册
    r := srv.GinEngine()
    
    r.GET("/hello", func(c *gin.Context) {
        c.JSON(200, gin.H{"msg": "world"})
    })

    // 3. 启动服务
    if err := srv.Start(); err != nil {
        panic(err)
    }
}
```

## ⚙️ 配置说明

```go
type ServerConfig struct {
    ServiceName  string        // 服务名称 (用于 Trace)
    Addr         string        // 监听地址
    Mode         string        // Gin 模式 (debug/release)
    Trace        bool          // 是否开启追踪
    Logger       *logger.Logger // 自定义 Logger
    EnableLogger bool          // 是否开启访问日志
    
    // 超时配置
    ReadTimeout  time.Duration
    WriteTimeout time.Duration
    IdleTimeout  time.Duration
}
```
