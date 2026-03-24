# Migration

这轮收敛的目标不是兼容优先，而是把库契约收干净。下面只记录行为变化明显、值得调用方关注的点。

## 总体规则

- 公开运行时 API 只要显式接收 `context.Context`，现在都按库契约要求非 nil；不再静默回退到 `context.Background()`。
- 配置和输入会在包边界统一做 `trim`、去重、克隆；空白字符串通常被当成“未设置”，不再被透传到底层 SDK。
- 指标不再依赖导入时全局注册。支持 metrics 的包优先采用按需注册、可注入 registry、可关闭的方式。

## telemetry/trace

- `Open(ctx, conf)` 现在是纯函数，只返回 `*sdktrace.TracerProvider`，不会再偷偷写全局 OpenTelemetry 状态。
- 需要安装全局 tracer/provider 时，改用 `InitTracer(ctx, conf)`。
- `Config.SampleRate` 现在是 `*float64`。
  - `nil` 表示使用默认值 `1.0`
  - `0` 表示 `NeverSample`
  - `1` 表示 `AlwaysSample`
- `Open` / `InitTracer` 现在都要求非 nil context。

## store/redisx

- `Config.DisableIdentity` 现在是 `*bool`，不再是 `bool`。
  - 这样才能区分“未设置默认行为”和“显式传入 false”
- `Open` / `Ping` 现在要求非 nil context。

示例：

```go
disableIdentity := false

client, err := redisx.Open(ctx, &redisx.Config{
    Addr:            "127.0.0.1:6379",
    DisableIdentity: &disableIdentity,
})
```

## store/gormx / store/redisx / contrib/mq/rmq

- 这些包的 metrics 现在都支持：
  - `DisableMetrics bool`
  - `MetricsRegisterer prometheus.Registerer`
- 如果你原来依赖默认全局 registry 自动出现指标，行为没有变；如果你需要隔离 registry，现在可以显式注入。

## contrib/auth/jwtx

- 只接受标准 HMAC 签名方法：`HS256`、`HS384`、`HS512`。
- `SecretKey` 会先 `trim`，空白值现在直接报错。
- `Generate` 会校验 claim 时间线；`ExpiresAt` 必须晚于 `IssuedAt` 和 `NotBefore`。
- `Issuer` 和 `Audience` 会被规范化和去重。

## contrib/mq/mqtt

- `Open`、`Publish`、`Subscribe`、`SubscribeMultiple`、`Unsubscribe` 都要求非 nil context。
- Broker、topic、filter 会先规范化；空白项会被过滤，重复项会去重。
- `SubscribeMultiple` 遇到规范化后重复的 topic 现在直接报错。
- 阿里云 MQTT 鉴权模式现在只接受 `Signature` 或 `Token`。

## contrib/mq/rmq

- `Producer.Start`、`Send`、`SendFIFO`、`SendDelay`、`Consumer.Start`、`Receive`、`Ack` 都要求非 nil context。
- `SubscriptionExpressions` 的 key 会先规范化；规范化后重复的 topic 现在直接报错。
- Producer 发送 FIFO / 延时消息时会克隆 `Message`，不再把 message group、delay timestamp 之类的状态回写到调用方消息对象。

## contrib/pay/alipay

- 所有运行时支付方法现在都要求非 nil context。
- `ParseNotify` 现在要求非 nil `*http.Request`。
- `SignType` 现在只接受 `RSA` 或 `RSA2`。
- 下载账单接口在支付宝未返回 `bill_download_url` 时会直接报错，不再返回空字符串。
- 通知验签必须显式提供证书模式或 `AlipayPublicKey`。

## contrib/pay/wechat

- `Open` / `New` 现在要求非 nil context。
- 所有预下单、查询、关单、退款 API 都要求非 nil context。
- 预下单请求里的 `Appid`、`Mchid`、`NotifyUrl`：
  - `nil` 或空白字符串都会被当成缺失值
  - 缺失时会回填 `Config` 里的默认值
  - 如果默认值本身也为空，则对应字段会被省略

## contrib/sms

- 发送和查询方法现在都要求非 nil context。
- 请求对象会先校验、克隆、清洗，再传给底层 SDK；调用方持有的请求对象不会被内部修改。

## contrib/discovery

- `ServerConfigs` 里的空白项和重复项会在边界被过滤。
- 现在必须提供“有效的 server config”或 `ClientConfig.Endpoint` 之一，空白占位不再算配置。

## store/opensearchx

- 所有运行时 API 都要求非 nil context。
- `Protocol` 现在只接受 `http` 或 `https`。
- `Request` 会克隆 query / headers map，避免后续调用方修改影响已发出的请求。

## store/ossx

- 所有运行时 API 都要求非 nil context。
- `PutObjectFromFile`、`AppendFile` 会先清洗文件路径、bucket、key。
- `Base` 配置会被克隆，避免调用方后续修改污染已构建客户端。
