# gormx

`gormx` 是对 GORM 的生产级封装。它不试图重新发明 ORM，而是把数据库打开、连接池、生命周期、日志、Prometheus 和 OpenTelemetry 边界清楚地整理起来。

## 设计原则

- 不在 `init()` 中注册 Prometheus 指标，指标统一按需懒注册。
- 默认禁用 GORM 自带日志，避免和 `gormx` 观测日志重复。
- 默认对 trace 和日志都只记录 SQL 模板，不插入绑定变量，避免泄露敏感数据。
- 默认在创建客户端后主动 `Ping`，让启动阶段尽早暴露 DSN、网络和权限问题。
- 成功查询默认走 `Debug`，慢查询走 `Warn`，失败查询走 `Error`，避免数据库访问日志把生产日志面淹没。

## 快速开始

```go
client, err := gormx.Open(context.Background(), &gormx.Config{
    Name:            "primary",
    Driver:          gormx.DriverMySQL,
    DSN:             "user:pass@tcp(127.0.0.1:3306)/app?parseTime=true",
    MaxIdleConns:    10,
    MaxOpenConns:    50,
    ConnMaxLifetime: time.Hour,
    Trace:           true,
    EnableLogger:    true,
})
if err != nil {
    panic(err)
}
defer client.Close()

db := client.WithContext(context.Background())

type User struct {
    ID    uint
    Name  string
    Email string
}

if err := db.AutoMigrate(&User{}); err != nil {
    panic(err)
}

if err := db.Create(&User{Name: "alice", Email: "alice@example.com"}).Error; err != nil {
    panic(err)
}
```

## API 摘要

```go
type Client interface {
    DB() *gorm.DB
    WithContext(context.Context) *gorm.DB
    SQLDB() *sql.DB
    Ping(context.Context) error
    Stats() sql.DBStats
    Use(gorm.Plugin) error
    Close() error
}
```

## 默认行为

- `Open` 在没有显式关闭时不会替你接管进程生命周期，调用方必须负责 `Close`
- `Open` 默认会 `Ping`；如果你明确要延迟连接，可以设置 `SkipPing`
- `Trace` 默认会启用 `WithoutQueryVariables` 和 `WithoutMetrics`
- `EnableLogger` 只控制成功/慢查询日志，失败查询始终记录
- `Driver` 目前内建支持 `mysql`、`postgres`、`sqlite`，复杂场景可以直接传 `Dialector`
