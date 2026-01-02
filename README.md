# Bang Micro

Bang Micro 是一个生产级别的 Go 微服务基础框架，旨在提供统一、高效、可观测的开发体验。

## 🌟 特性

*   **统一规范**：全模块采用 `x` 后缀命名（如 `httpx`, `grpcx`），避免命名冲突，风格统一。
*   **生产就绪**：内置 OpenTelemetry 链路追踪、Prometheus 监控指标、结构化日志。
*   **弹性设计**：默认集成超时控制、Panic 恢复、连接池优化。
*   **开箱即用**：提供 HTTP、gRPC、Redis、MySQL、Elasticsearch 等常用组件的深度封装。

## 📦 模块概览

### Transport (通信层)
*   [**httpx**](transport/httpx/README.md): 增强型 HTTP 客户端与服务端，支持拦截器和自动追踪。
*   [**grpcx**](transport/grpcx/README.md): 生产级 gRPC 封装，集成拦截器链。
*   [**ginx**](transport/ginx/README.md): 基于 Gin 的 Web 框架封装，集成统一中间件。
*   [**tcpx**](transport/tcpx/README.md): 高性能 TCP 框架。
*   [**wsx**](transport/wsx/README.md): WebSocket 服务端与客户端封装。

### Store (存储层)
*   [**gormx**](store/gormx/README.md): GORM 封装，集成 Tracing 和 Metrics。
*   [**redisx**](store/redisx/README.md): Redis 客户端封装。
*   [**elasticsearchx**](store/elasticsearchx/README.md): Elasticsearch 客户端。
*   [**opensearchx**](store/opensearchx/README.md): Aliyun OpenSearch 客户端。

### Conf (配置层)
*   [**viperx**](conf/viperx/README.md): 基于 Viper 的配置加载器，支持环境变量覆盖。
*   [**envx**](conf/envx/README.md): 环境检测工具 (Dev/Test/Prod)。

## 🚀 快速开始

### 安装

```bash
go get github.com/bang-go/micro
```

### 示例：启动一个 HTTP 服务

```go
package main

import (
    "github.com/bang-go/micro/transport/ginx"
    "github.com/gin-gonic/gin"
)

func main() {
    // 1. 创建 Server
    server := ginx.New(&ginx.ServerConfig{
        Addr: ":8080",
        Name: "my-service",
    })

    // 2. 注册路由
    server.Use(func(c *gin.Context) {
        // 全局中间件
        c.Next()
    })
    
    g := server.GinEngine()
    g.GET("/ping", func(c *gin.Context) {
        c.JSON(200, gin.H{"message": "pong"})
    })

    // 3. 启动
    if err := server.Start(); err != nil {
        panic(err)
    }
}
```

## 🛠️ 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 License

MIT
