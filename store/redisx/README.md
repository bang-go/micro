# redisx

`redisx` 是基于 `go-redis/v9` 的增强封装，专注于微服务场景下的可观测性和易用性。

## ✨ 特性

*   **自动 Hook**：自动注入 Tracing Hook 和 Metrics Hook。
*   **链路追踪**：每个 Redis 操作都会生成 Span，记录命令和耗时。
*   **监控指标**：自动收集连接池状态、命中率、命令延迟等指标。
*   **配置简化**：支持单机、哨兵、集群模式的统一配置。

## 🚀 快速开始

### 初始化

```go
import "github.com/bang-go/micro/store/redisx"

func main() {
    client, err := redisx.New(&redisx.Config{
        Addr:         "localhost:6379",
        Password:     "123456",
        DB:           0,
        Trace:        true, // 开启追踪
        EnableMetric: true, // 开启监控
    })
    if err != nil {
        panic(err)
    }

    // 使用标准 go-redis API
    err = client.Set(ctx, "key", "value", 0).Err()
    val, err := client.Get(ctx, "key").Result()
}
```

## ⚙️ 配置说明

```go
type Config struct {
    Addr         string
    Username     string
    Password     string
    DB           int
    MaxRetries   int
    MinIdleConns int
    PoolSize     int
    
    Trace        bool // OpenTelemetry Tracing
    EnableMetric bool // Prometheus Metrics
}
```
