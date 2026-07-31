# wsx

`wsx` 提供一个职责明确的 WebSocket 服务端运行时：

- `Endpoint` 负责 HTTP Upgrade、连接注册、连接清理和停机。
- `Session` 负责一次认证后的连接业务生命周期。
- `Connect` 负责单条物理连接的读、写和立即关闭。
- `Hub` 负责本地或 Redis 多节点下的用户、房间路由。
- `MessageBroker` 只负责 Hub 的跨节点命令传递。

包内不提供客户端、不管理业务认证状态，也不推断重连策略。

## Endpoint

认证和身份解析必须在 Upgrade 前一次完成，并返回不可变的 `Session`：

```go
hub, err := wsx.NewHub(nil)
if err != nil {
    return err
}

endpoint, err := wsx.NewEndpoint(wsx.EndpointConfig{
    Logger:            log,
    HeartbeatInterval: 25 * time.Second,
    WriteTimeout:      10 * time.Second,
}, hub, func(ctx context.Context, r *http.Request) (wsx.Session, error) {
    claims, err := authenticate(ctx, r)
    if err != nil {
        return nil, err
    }
    return &connectionSession{claims: *claims}, nil
})
if err != nil {
    return err
}

if err := endpoint.Start(ctx); err != nil {
    return err
}

mux := http.NewServeMux()
mux.Handle("/ws", endpoint)
```

`Session.Serve` 是连接期唯一业务入口：

```go
func (s *connectionSession) UserID() string {
    return s.claims.App + ":" + s.claims.SubjectID
}

func (s *connectionSession) Serve(ctx context.Context, conn wsx.Connect) error {
    for {
        _, payload, err := conn.ReadMessage(ctx)
        if err != nil {
            return nil
        }
        if err := conn.WriteMessage(ctx, websocket.MessageBinary, payload); err != nil {
            return err
        }
    }
}
```

`Endpoint.Shutdown(ctx)` 会停止连接生命周期、立即关闭全部物理连接、等待连接清理完成，再关闭 Hub 和 broker。

默认 Origin 策略只接受无 `Origin` 或严格同源请求。跨源策略必须通过 `EndpointConfig.CheckOrigin` 显式提供。

## 连接终止与关闭

`terminated` 和物理 `close` 是两个不同事实：

- 读、写或心跳失败会终止连接生命周期并取消 `Session.Serve` 的 context。
- `CloseNow` 负责底层 transport 资源关闭，并且只执行一次。
- 即使连接已经 terminated，`CloseNow` 仍会完成底层关闭。

服务端不发送第二次 WebSocket close handshake，也不把对端正常断开记录成服务端关闭错误。

## Hub

```go
hub, err := wsx.NewHub(broker)
if err != nil {
    return err
}

_ = hub.Broadcast(ctx, packet)
_ = hub.SendToUser(ctx, userID, packet)
_ = hub.BroadcastToRoom(ctx, room, packet)
_ = hub.DisconnectUser(ctx, userID, finalPacket)
_ = hub.DisconnectRoom(ctx, room, finalPacket)
_ = hub.JoinSessionToRoom(ctx, connectionID, room)
_ = hub.JoinUserToRoom(ctx, userID, room)
_ = hub.LeaveUserFromRoom(ctx, userID, room)
```

语义约束：

- `UserID` 是握手后不可变的业务路由身份。
- `SessionID` 是 wsx 生成的物理连接 ID。
- `JoinSessionToRoom` 只修改一条物理连接。
- `JoinUserToRoom` / `LeaveUserFromRoom` 修改该用户当时存在的全部连接。
- `DisconnectUser` / `DisconnectRoom` 在同一命令中先同步写入可选 final message，再立即关闭目标连接。
- 分布式命令等待 Redis 实际订阅节点的 ACK；未提供更短 deadline 时受 Hub command timeout 限制。

`Endpoint` 拥有 Hub 生命周期。传给 `NewHub` 的 broker 也随 Hub 一起关闭，不应被其他 Hub 共享。

## Redis Broker

```go
broker, err := wsx.NewRedisBroker("127.0.0.1:6379", "", 0)
if err != nil {
    return err
}
hub, err := wsx.NewHub(broker)
```

也可以注入已有 Redis client：

```go
broker, err := wsx.NewRedisBrokerWithClient(client)
```

broker 会关闭自己创建的 Redis client；注入的 client 生命周期仍由调用方管理。每次 `Subscribe` 都返回独立 `Subscription`，调用方通过 `Subscription.Close(ctx)` 显式结束订阅。
