# Bang Micro

Bang Micro 是一个面向生产环境的 Go 类库集，不是把所有能力塞进一个“大框架”。每个包都保持独立、显式生命周期和清晰边界，目标是让业务可以直接使用 `micro`，而不必先理解每个第三方 SDK 的坑点和默认行为。

## 设计取向

- 不依赖导入时副作用。能不在 `init()` 做的事情，就不在 `init()` 做。
- 公开 API 的生命周期显式化，启动、关闭、全局安装和请求级调用分开处理。
- 对外运行时 API 统一要求非 nil `context.Context`，不静默回退到 `context.Background()`。
- 在包边界处清洗和克隆调用方输入，避免把调用方对象当成内部可变状态。
- Prometheus 指标优先按需注册、可注入、可关闭，避免污染全局默认 registry。
- 保留底层 SDK 能力，但用更干净的默认行为、日志、trace 和错误边界包起来。

## 文档

- [文档索引](docs/README.md)
- [迁移说明](docs/MIGRATION.md)

## 模块概览

### Transport

- [httpx](transport/httpx/README.md): 生产级 `net/http` 客户端与服务端封装。
- [grpcx](transport/grpcx/README.md): 干净的 gRPC 客户端、服务端与拦截器边界。
- [ginx](transport/ginx/README.md): 基于 Gin 的服务端封装，统一 trace、metrics、recovery。
- [wsx](transport/wsx/README.md): WebSocket 客户端、服务端、Hub、房间广播与订阅模型。
- [tcpx](transport/tcpx/README.md): 显式生命周期的 TCP 客户端与服务端封装。
- [udpx](transport/udpx/README.md): 面向 datagram 的 UDP 封装。

### Store

- [gormx](store/gormx/README.md): GORM 打开、连接池、trace、metrics、日志边界。
- [redisx](store/redisx/README.md): `go-redis/v9` 单节点客户端封装。
- [elasticsearchx](store/elasticsearchx/README.md): Elasticsearch v9 客户端封装。
- [opensearchx](store/opensearchx/README.md): 阿里云 OpenSearch 查询与搜索封装。
- [ossx](store/ossx/README.md): 阿里云 OSS 客户端封装。

### Conf

- [viperx](conf/viperx/README.md): 配置加载与环境变量覆盖。
- [envx](conf/envx/README.md): 环境模式识别与主机名辅助工具。

### Telemetry

- [trace](telemetry/trace/README.md): OpenTelemetry TracerProvider 初始化与全局安装。
- [logger](telemetry/logger/README.md): 结构化日志与 trace 上下文注入。

### Contrib

- [jwtx](contrib/auth/jwtx/README.md): 泛型 JWT 签发与解析封装。
- [discovery](contrib/discovery/README.md): Nacos naming client 封装。
- [mqtt](contrib/mq/mqtt/README.md): MQTT 客户端封装，兼容阿里云 MQTT 鉴权模式。
- [rmq](contrib/mq/rmq/README.md): RocketMQ 5 Producer / SimpleConsumer 封装。
- [alipay](contrib/pay/alipay/README.md): 支付宝支付封装。
- [wechat](contrib/pay/wechat/README.md): 微信支付 v3 封装。
- [sms](contrib/sms/README.md): 阿里云短信封装。
- [umeng](contrib/push/umeng/README.md): 友盟推送封装。

### Runtime

- [pool](pkg/pool/README.md): 有界 goroutine 池与显式背压模型。

## 安装

```bash
go get github.com/bang-go/micro
```

## 使用建议

- 只想拿到 `TracerProvider` 而不改全局，用 `telemetry/trace.Open`；需要设置全局 tracer，再用 `telemetry/trace.InitTracer`。
- 需要隔离 Prometheus registry 的包，优先注入 `MetricsRegisterer`；明确不需要指标时，用 `DisableMetrics`。
- 如果公开方法接收 `context.Context`，就传业务上下文，不要传 nil。
- 如果你已经熟悉底层 SDK，可以直接拿原始客户端；大多数封装都保留了 `Raw()` 或等价入口。

## License

MIT
