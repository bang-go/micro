# httpx

`httpx` 是基于 `net/http` 的增强型 HTTP 客户端封装，专为微服务环境设计。

## ✨ 特性

*   **自动追踪**：集成 OpenTelemetry，自动注入 Trace Context。
*   **指标监控**：集成 Prometheus，自动记录请求耗时、状态码分布。
*   **连接池优化**：预设生产级连接池参数（MaxIdleConns, IdleConnTimeout 等）。
*   **结构化日志**：记录请求/响应的关键信息。
*   **易用 API**：提供 Fluent API 风格的请求构建。

## 🚀 快速开始

### 1. 初始化客户端

```go
import "github.com/bang-go/micro/transport/httpx"

// 使用默认配置
client := httpx.New(nil)

// 或者自定义配置
client := httpx.New(&httpx.Config{
    Timeout:      5 * time.Second,
    Trace:        true, // 开启链路追踪
    EnableLogger: true, // 开启访问日志
})
```

### 2. 发起请求

```go
ctx := context.Background()

req := &httpx.Request{
    Method:      httpx.MethodPost,
    Url:         "https://api.example.com/users",
    ContentType: httpx.ContentJson,
    Body:        httpx.FormatJsonData(map[string]string{"name": "Alice"}),
}

resp, err := client.Send(ctx, req)
if err != nil {
    // handle error
}

fmt.Printf("Status: %d, Body: %s", resp.StatusCode, string(resp.Content))
```

## ⚙️ 配置说明

```go
type Config struct {
    Timeout      time.Duration // 请求超时时间
    Trace        bool          // 是否开启 OpenTelemetry 追踪
    Logger       *logger.Logger // 自定义 Logger
    EnableLogger bool          // 是否开启日志记录
    
    // 连接池配置
    MaxIdleConns        int
    MaxIdleConnsPerHost int
    MaxConnsPerHost     int
    IdleConnTimeout     time.Duration
}
```
