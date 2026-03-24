# redisx

`redisx` 是对 `go-redis/v9` 单节点客户端的生产级封装。它把连接建立、启动探活、日志、Prometheus 指标和 OpenTelemetry trace 边界整理干净，同时保留原生 `go-redis` API。

## 设计原则

- 不在 `init()` 中注册 Prometheus 指标，指标统一按需懒注册。
- 默认在创建客户端后主动 `PING`，尽早暴露地址、认证和网络问题。
- 启动探活和连接管理命令（如 `HELLO`、`AUTH`、`CLIENT SETINFO`）不进入业务日志、指标和 trace，避免把连接噪音混进业务观测面。
- 成功命令默认走 `Debug`，慢命令走 `Warn`，失败命令走 `Error`，避免把日志面打爆。
- 日志不记录命令参数和值，只记录命令名、参数数量、耗时和结果，避免泄露 token、session、业务键值。
- Trace 默认不写入完整命令参数，避免把 key/value 带进 span。

## 快速开始

```go
disableIdentity := true

client, err := redisx.Open(context.Background(), &redisx.Config{
    Name:           "cache-primary",
    Addr:           "127.0.0.1:6379",
    Password:       "",
    DB:             0,
    Trace:          true,
    EnableLogger:   true,
    DisableIdentity: &disableIdentity,
})
if err != nil {
    panic(err)
}
defer client.Close()

rdb := client.Redis()

if err := rdb.Set(context.Background(), "key", "value", 0).Err(); err != nil {
    panic(err)
}

val, err := rdb.Get(context.Background(), "key").Result()
fmt.Println(val, err)
```

## API 摘要

```go
type Client interface {
    Redis() *redis.Client
    Options() *redis.Options
    Ping(context.Context) error
    Stats() redis.PoolStats
    AddHook(redis.Hook) error
    Close() error
}
```

## 默认行为

- `Open` 默认会 `PING`；如果你明确要延迟连接，可以设置 `SkipPing`
- `DisableIdentity` 默认开启；如果你要显式关闭，需要传入一个值为 `false` 的指针，这样配置语义才不会和零值冲突
- `Trace` 默认不包含完整命令参数；如果你确实需要，可以设置 `TraceIncludeCommandArgs`
- 如果你需要更底层控制，可以直接传 `Options`
