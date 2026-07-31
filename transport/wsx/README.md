# wsx

`wsx` 提供 WebSocket 服务端、客户端、连接抽象和本地/分布式 Hub。

## 设计原则

- 默认安全：服务端默认仅允许同源或无 `Origin` 的升级请求。
- 生命周期清晰：`Client.Start(ctx)` 的 `ctx` 仅用于首连截止时间；`Server.Start(ctx)` 的 `ctx` 则负责服务生命周期。
- 关闭可收口：`Server.Shutdown(ctx)` 会主动关闭所有已升级连接，而不是只关闭 HTTP listener。
- 分布式语义正确：`RedisBroker` 使用单一读取循环分发消息，避免多 goroutine 竞争同一个 PubSub 流。
- 用户定向投递按用户路由：配置 broker 后，`SendTo` / `Kick` 只会发送到持有该用户连接的节点。
- 会话定向投递按 session 路由：配置 broker 后，`SendToSession` / `KickSession` 只会发送到持有该 session 连接的节点。
- 房间语义干净：房间成员关系是 `Session` 级别，不会把同一用户的其他设备/标签页一并加入。
- 房间广播按房间路由：配置 broker 后，房间消息只会发送到订阅该房间的节点，而不是全节点扩散。
- 可观测性无副作用：Prometheus 指标改为懒注册，不在包导入时污染全局注册表。
- 内部错误可观测：无调用方返回通道的 ack 发布失败和 Redis broker 内部错误会进入指标，不静默消失。
- 身份不可变：连接的 `UserID` 在握手阶段确定，建立后不可再变更，保证 Hub 索引一致性。
- 关闭结果与终止原因分离：`ReadMessage` 保留 normal close、EOF、connection reset 等真实终止原因；`Connect.Close` 只报告连接资源是否完成收口，对已经终止的 transport 幂等成功。

## Server

```go
server := wsx.NewServer(&wsx.ServerConfig{
    Addr:       ":8080",
    Trace:      true,
    EnableLogger: true,
})

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go func() {
    _ = server.Start(ctx, func(ctx context.Context, conn wsx.Connect) {
        for {
            _, payload, err := conn.ReadMessage(ctx)
            if err != nil {
                return
            }

            _ = conn.SendBinary(ctx, payload)
        }
    })
}()
```

可选项：

- `WithServerCheckOrigin` 自定义 origin 校验策略
- `WithServerBeforeUpgrade` 在升级前执行鉴权
- `WithServerIdentify` 在升级前提取业务用户标识
- `WithServerOnConnect` 在连接建立后执行副作用逻辑
- `WithServerHub` 自动把连接注册到 Hub

## Client

```go
client := wsx.NewClient(
    "ws://127.0.0.1:8080/ws",
    wsx.WithClientReconnectInterval(2*time.Second),
)

client.OnMessage(func(ctx context.Context, mt websocket.MessageType, payload []byte) {
    fmt.Printf("recv: %s\n", payload)
})

client.OnDisconnect(func(ctx context.Context, err error) {
    fmt.Printf("disconnect: %v\n", err)
})

if err := client.Start(context.Background()); err != nil {
    panic(err)
}
defer func() {
    _ = client.Close()
}()
```

特性：

- 首次连接失败时会按重连策略继续尝试，直到成功、超过上限或 `Start(ctx)` 超时
- `Close()` 幂等并返回底层连接关闭错误
- 同一个 `Client` 实例只允许启动一次，避免多重后台循环

## Hub

```go
hub, err := wsx.NewHub()
if err != nil {
    panic(err)
}
hub.Register(conn)
_ = hub.Broadcast(context.Background(), []byte("hello"))
_ = hub.SendTo(context.Background(), userID, []byte("private"))
_ = hub.SendToSession(context.Background(), conn.SessionID(), []byte("session-private"))
_ = hub.KickSession(context.Background(), conn.SessionID())
_ = hub.Join(context.Background(), conn.SessionID(), roomID)
_ = hub.BroadcastToRoom(context.Background(), roomID, []byte("room-msg"))
_ = hub.KickRoom(context.Background(), roomID)
```

`Hub` 默认是本地内存实现；配置 `WithHubBroker` 后，广播走全局控制通道，单播/踢人走用户级通道，session 级发送/踢线走 session 级通道，房间广播/房间踢线走房间级通道。房间成员关系仍然是本地 Session 状态，由接入该连接的节点负责维护。所有分布式/本地操作都会等待目标节点执行确认并返回错误，不再静默吞掉失败；调用方未设置 deadline 时，Hub 使用默认命令超时防止无限等待。

`Hub.Register` 要求连接提供非空且唯一的 `SessionID()`。重复注册或重复 session 会直接返回错误，避免覆盖 session 索引后出现不可精准踢线的隐性状态。

服务端 `Connect.Close()` 是幂等、非协商式的资源收口操作：活动 transport 会立即关闭，不等待对端参与关闭握手，保证 Hub Kick 与 Server Shutdown 不受对端读取状态影响；已经由读、写或心跳链路判定终止的 transport 只执行 wsx 本地清理。对端先关闭不是 `Close()` 失败，原始终止原因仍由 `ReadMessage` 返回，调用方不得用 `Close()` 的 nil 结果反推连接此前属于 normal closure。需要主动发送 normal close frame 的客户端应在自己的连接生命周期内完成该动作。

## Redis Broker

```go
broker := wsx.NewRedisBroker("127.0.0.1:6379", "", 0)
defer func() {
    _ = broker.Close()
}()
```

`RedisBroker.Subscribe` 的订阅生命周期绑定到传入的 `context.Context`。当 `ctx.Done()` 触发时，对应 handler 会被自动移除。`Hub.Close()` 不会隐式关闭外部 broker，broker 生命周期由调用方显式管理。`Hub.Close()` 幂等并返回底层连接关闭错误。
