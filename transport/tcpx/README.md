# tcpx

`tcpx` 是面向生产环境的原生 TCP 封装。它不试图发明协议层抽象，而是把连接读写、客户端拨号、服务端生命周期、指标与 panic 边界做干净。

## 设计原则

- 连接抽象保留 `net.Conn` 的能力，但修正了旧版 `Send/Receive` 无法表达部分读写的问题。
- 客户端与服务端配置拆分，分别管理拨号和监听生命周期。
- 不在 `init()` 中注册 Prometheus 指标，指标统一按需懒注册，并支持注入自定义 `prometheus.Registerer`。
- 服务端具备显式 `Start` / `Serve` / `Shutdown` / `Close` 生命周期，并对运行态、最大连接数和 panic recovery 做边界控制。
- 读超时与写超时分离，避免单一超时配置同时污染所有方向的 I/O 语义。
- `DialContext` / `Start` / `Serve` / `Shutdown` 都要求显式传入非 `nil` context。

## Client

```go
client := tcpx.NewClient(&tcpx.ClientConfig{
    Addr:         "127.0.0.1:9000",
    DialTimeout:  3 * time.Second,
    ReadTimeout:  5 * time.Second,
    WriteTimeout: 5 * time.Second,
    Trace:        true,
    EnableLogger: true,
})

conn, err := client.DialContext(context.Background())
if err != nil {
    panic(err)
}
defer conn.Close()

if err := conn.WriteFull([]byte("ping")); err != nil {
    panic(err)
}

reply := make([]byte, 4)
if err := conn.ReadFull(reply); err != nil {
    panic(err)
}

fmt.Println(string(reply))
```

如果你需要无网络单测、unix socket 适配或自定义拨号链路，可以提供 `ContextDialer`。当同时启用 `TLSConfig` 时，`tcpx` 会沿用目标地址自动补齐 `ServerName`，行为与标准 TLS 拨号保持一致。

## Server

```go
server := tcpx.NewServer(&tcpx.ServerConfig{
    Addr:            ":9000",
    ReadTimeout:     30 * time.Second,
    WriteTimeout:    30 * time.Second,
    MaxConnections:  4096,
    ShutdownTimeout: 10 * time.Second,
    Trace:           true,
    EnableLogger:    true,
})

handler := tcpx.HandlerFunc(func(ctx context.Context, conn tcpx.Connect) error {
    buf := make([]byte, 4)
    if err := conn.ReadFull(buf); err != nil {
        return err
    }
    if string(buf) == "ping" {
        return conn.WriteFull([]byte("pong"))
    }
    return nil
})

go func() {
    if err := server.Start(context.Background(), handler); err != nil {
        panic(err)
    }
}()

defer server.Shutdown(context.Background())
```

默认行为：

- `Shutdown` 在没有 deadline 时会自动应用 `ShutdownTimeout`
- 关闭服务端时会停止接受新连接，并主动关闭活动连接以终止阻塞 I/O
- handler panic 会被恢复、记录结构化日志和堆栈，并关闭该连接
- 客户端拨号和服务端连接指标都只会按需注册一次；也可以通过 `MetricsRegisterer` 注入到独立 registry，或通过 `DisableMetrics` 完全关闭

## API 摘要

```go
type Connect interface {
    Read([]byte) (int, error)
    ReadFull([]byte) error
    Write([]byte) (int, error)
    WriteFull([]byte) error
    SetReadTimeout(time.Duration)
    SetWriteTimeout(time.Duration)
    LocalAddr() net.Addr
    RemoteAddr() net.Addr
    Conn() net.Conn
    Close() error
}

type Client interface {
    Dial() (Connect, error)
    DialContext(context.Context) (Connect, error)
}

type Server interface {
    Start(context.Context, Handler) error
    Serve(context.Context, net.Listener, Handler) error
    Shutdown(context.Context) error
    Close() error
    Listener() net.Listener
    Use(...Interceptor) error
}
```

## 配置补充

```go
type ClientConfig struct {
    MetricsRegisterer prometheus.Registerer // 可选，默认使用 prometheus.DefaultRegisterer
    DisableMetrics    bool
}

type ServerConfig struct {
    MetricsRegisterer prometheus.Registerer // 可选，默认使用 prometheus.DefaultRegisterer
    DisableMetrics    bool
}
```
