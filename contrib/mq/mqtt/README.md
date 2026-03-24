# mqtt

`mqtt` 是对 Eclipse Paho MQTT 客户端的生产级封装。它把连接建立、主题规范化、超时边界和阿里云 MQTT 鉴权做成更干净的库契约。

## 设计原则

- `Open` 显式接收 context，用来约束连接阶段超时和取消。
- broker、topic、filter 会在边界做规范化、去重和校验。
- 运行时 API 统一要求非 nil context。
- 保留原始客户端访问能力，但不把原始 token 机制暴露给业务层。
- 阿里云 MQTT 鉴权模式只接受明确支持的值。

## 快速开始

```go
cli, err := mqtt.Open(context.Background(), &mqtt.Config{
    Brokers:  []string{"tcp://127.0.0.1:1883"},
    ClientID: "orders-worker-1",
    Username: "service",
    Password: "secret",
})
if err != nil {
    panic(err)
}
defer cli.Disconnect(250)

if err := cli.Publish(context.Background(), "orders.created", 1, false, []byte("hello")); err != nil {
    panic(err)
}
```

阿里云 MQTT 鉴权可以直接交给包内辅助逻辑：

```go
cli, err := mqtt.New(&mqtt.Config{
    Brokers: []string{"tcp://mqtt.example.com:1883"},
    Aliyun: &mqtt.AliyunAuth{
        Mode:            mqtt.AuthModeSignature,
        AccessKeyID:     "ak",
        AccessKeySecret: "sk",
        InstanceID:      "instance-id",
        GroupID:         "GID_orders",
        DeviceID:        "worker-1",
    },
})
```

## API 摘要

```go
type Client interface {
    Raw() pahomqtt.Client
    IsConnected() bool
    Disconnect(quiesce uint)
    Publish(context.Context, string, byte, bool, any) error
    Subscribe(context.Context, string, byte, MessageHandler) error
    SubscribeMultiple(context.Context, map[string]byte, MessageHandler) error
    Unsubscribe(context.Context, ...string) error
    AddRoute(string, MessageHandler) error
}

func Open(context.Context, *Config) (Client, error)
func New(*Config) (Client, error)
func BuildUsername(string, string, string) string
func BuildSignaturePassword(string, string) string
func BuildClientID(string, string) string
func IsTimeout(error) bool
```

## 默认行为

- `ConnectTimeout <= 0` 时默认 `30s`
- `OperationWait <= 0` 时默认 `30s`
- `ProtocolVersion == 0` 时默认 `4`
- `New(conf)` 是 `Open(context.Background(), conf)` 的便捷形式
- `SubscribeMultiple` 遇到规范化后重复的 topic 会直接报错
