# udpx

`udpx` 是面向生产环境的 UDP 封装。它按 datagram 语义建模，不把 UDP 伪装成流式连接，同时把客户端拨号、服务端生命周期、可观测性和 panic 边界做清楚。

## 设计原则

- 连接抽象保留 UDP 的报文边界，使用 `Read/ReadFrom/Write/WriteTo`，不引入错误的 `ReadFull/WriteFull` 语义。
- 服务端 handler 面向 `Packet` 抽象，而不是直接暴露共享的 `*net.UDPConn`。
- 不在 `init()` 中注册 Prometheus 指标，指标统一按需懒注册。
- `DialContext` / `Start` / `Serve` / `Shutdown` / `Packet.WriteTo` 都要求显式传入非 nil `context.Context`，避免静默吞掉调用方生命周期。
- 服务端具备显式 `Start` / `Serve` / `Shutdown` / `Close` 生命周期和运行态保护。
- 对真实 UDP socket 会检测报文截断并直接丢弃，避免把被截断的数据悄悄交给业务。

## Client

```go
client := udpx.NewClient(&udpx.ClientConfig{
    Addr:         "127.0.0.1:9001",
    DialTimeout:  3 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
    Trace:        true,
    EnableLogger: true,
})

conn, err := client.DialContext(context.Background())
if err != nil {
    panic(err)
}
defer conn.Close()

if _, err := conn.Write([]byte("ping")); err != nil {
    panic(err)
}

buf := make([]byte, 1024)
n, err := conn.Read(buf)
if err != nil {
    panic(err)
}

fmt.Println(string(buf[:n]))
```

如果你需要无网络单测、自定义拨号链路或 unixgram 适配，可以提供 `ContextDialer`。

指标默认注册到 `prometheus.DefaultRegisterer`；如果你需要隔离 registry，可以通过 `MetricsRegisterer` 注入，或者用 `DisableMetrics` 完全关闭。

## Server

```go
server := udpx.NewServer(&udpx.ServerConfig{
    Addr:            ":9001",
    MaxPacketSize:   64 * 1024,
    MaxConcurrency:  256,
    ShutdownTimeout: 10 * time.Second,
    Trace:           true,
    EnableLogger:    true,
})

handler := udpx.HandlerFunc(func(ctx context.Context, packet udpx.Packet) error {
    if string(packet.Payload()) == "ping" {
        _, err := packet.Reply(ctx, []byte("pong"))
        return err
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

- `DialContext` / `Start` / `Serve` / `Shutdown` / `Packet.Reply` / `Packet.WriteTo` 都要求非 nil context
- `Shutdown` 在没有 deadline 时会自动应用 `ShutdownTimeout`
- 关闭服务端时会停止读取新报文，并取消所有正在处理的 packet context
- handler panic 会被恢复、记录结构化日志和堆栈
- 自定义 `PacketConn` 和 `ContextDialer` 都支持无网络、确定性的测试

## API 摘要

```go
type Connect interface {
    Read([]byte) (int, error)
    ReadFrom([]byte) (int, net.Addr, error)
    Write([]byte) (int, error)
    WriteTo([]byte, net.Addr) (int, error)
    SetReadTimeout(time.Duration)
    SetWriteTimeout(time.Duration)
    LocalAddr() net.Addr
    RemoteAddr() net.Addr
    Conn() net.Conn
    Close() error
}

type Packet interface {
    Payload() []byte
    RemoteAddr() net.Addr
    LocalAddr() net.Addr
    Reply(context.Context, []byte) (int, error)
    WriteTo(context.Context, []byte, net.Addr) (int, error)
    PacketConn() net.PacketConn
}

type Client interface {
    Dial() (Connect, error)
    DialContext(context.Context) (Connect, error)
}

type Server interface {
    Start(context.Context, Handler) error
    Serve(context.Context, net.PacketConn, Handler) error
    Shutdown(context.Context) error
    Close() error
    PacketConn() net.PacketConn
    Use(...Interceptor) error
}
```
