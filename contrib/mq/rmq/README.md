# rmq

`rmq` 是对 RocketMQ 5 Go SDK 的 Producer 和 SimpleConsumer 封装。重点在于把启动、发送、接收、日志、指标和错误边界拆干净，而不是再造一层消息模型。

## 设计原则

- Producer 和 Consumer 生命周期显式，统一 `Start` / `Close`。
- 运行时 API 统一要求非 nil context。
- 指标按需注册，可关闭、可注入 registry。
- 发送时会克隆调用方消息，避免 FIFO / 延时发送把内部状态回写给业务对象。
- 订阅表达式在边界做规范化和重复检查。

## 快速开始

```go
producer, err := rmq.NewProducer(&rmq.ProducerConfig{
    Name:      "orders-producer",
    Endpoint:  "127.0.0.1:8081",
    AccessKey: "ak",
    SecretKey: "sk",
})
if err != nil {
    panic(err)
}
defer producer.Close()

if err := producer.Start(context.Background()); err != nil {
    panic(err)
}

_, err = producer.Send(context.Background(), &rmq.Message{
    Topic: "orders",
    Body:  []byte("created"),
})
if err != nil {
    panic(err)
}
```

```go
consumer, err := rmq.NewSimpleConsumer(&rmq.ConsumerConfig{
    Name:     "orders-consumer",
    Group:    "GID_orders",
    Endpoint: "127.0.0.1:8081",
    SubscriptionExpressions: map[string]*rmq.FilterExpression{
        "orders": rmq.SubAll,
    },
})
if err != nil {
    panic(err)
}
defer consumer.Close()

if err := consumer.Start(context.Background()); err != nil {
    panic(err)
}

messages, err := consumer.Receive(context.Background())
if err != nil {
    panic(err)
}
for _, message := range messages {
    if err := consumer.Ack(context.Background(), message); err != nil {
        panic(err)
    }
}
```

## API 摘要

```go
func NewProducer(*ProducerConfig) (Producer, error)
func NewSimpleConsumer(*ConsumerConfig) (Consumer, error)

type Producer interface {
    Start(context.Context) error
    Close() error
    Send(context.Context, *Message) ([]*SendReceipt, error)
    SendAsync(context.Context, *Message, AsyncSendHandler)
    SendFIFO(context.Context, *Message, string) ([]*SendReceipt, error)
    SendDelay(context.Context, *Message, time.Time) ([]*SendReceipt, error)
}

type Consumer interface {
    Start(context.Context) error
    Receive(context.Context) ([]*MessageView, error)
    Ack(context.Context, *MessageView) error
    Close() error
}
```

## 默认行为

- `StartTimeout <= 0` 时默认 `30s`
- Consumer `AwaitDuration <= 0` 时默认 `5s`
- Consumer `InvisibleDuration <= 0` 时默认 `20s`
- Consumer `MaxMessageNum <= 0` 时默认 `16`，上限 `32`
- `Topic` 和 `SubscriptionExpressions` 二选一；如果只给 `Topic`，会自动订阅 `SubAll`
