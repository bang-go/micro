package rmq

import (
	"context"
	"errors"
	"fmt"
	"time"

	rmqClient "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/apache/rocketmq-clients/golang/v5/credentials"
	v2 "github.com/apache/rocketmq-clients/golang/v5/protocol/v2"
	"github.com/bang-go/util"
)

const (
	DefaultConsumerAwaitDuration           = time.Second * 5
	DefaultConsumerMaxMessageNum     int32 = 16
	DefaultConsumerInvisibleDuration       = time.Second * 20
	ConsumerMaxMessageNum            int32 = 32 //rocketmq sdk限制最大只能是32
	// DefaultConsumerStartTimeout 默认的 Consumer Start() 超时时间
	DefaultConsumerStartTimeout = time.Second * 10
)

type SimpleConsumer = rmqClient.SimpleConsumer
type MessageView = rmqClient.MessageView
type FilterExpression = rmqClient.FilterExpression
type MessageViewFunc func(*MessageView) bool
type ErrRpcStatus = rmqClient.ErrRpcStatus

var AsErrRpcStatus = rmqClient.AsErrRpcStatus
var NewFilterExpression = rmqClient.NewFilterExpression
var NewFilterExpressionWithType = rmqClient.NewFilterExpressionWithType
var SubAll = rmqClient.SUB_ALL

const (
	CodeMessageNotFound = v2.Code_MESSAGE_NOT_FOUND
)

type Consumer interface {
	Start() error
	Receive() ([]*MessageView, error)
	ReceiveWithContext(ctx context.Context) ([]*MessageView, error)
	GetSimpleConsumer() SimpleConsumer
	Ack(ctx context.Context, messageView *MessageView) error
	Close() error
}

type consumerEntity struct {
	simpleConsumer SimpleConsumer
	*ConsumerConfig
}
type ConsumerConfig struct {
	Topic                   string
	Group                   string
	Endpoint                string
	AccessKey               string
	SecretKey               string
	SubscriptionExpressions map[string]*FilterExpression
	AwaitDuration           time.Duration // maximum waiting time for receive func
	MaxMessageNum           int32         // maximum number of messages received at one time
	InvisibleDuration       time.Duration // invisibleDuration should > 20s
	// StartTimeout Start() 方法的超时时间
	// 如果为 0，则使用默认值 DefaultConsumerStartTimeout (10秒)
	// 注意：这是应用层的超时控制，用于防止 Start() 方法无限阻塞
	// 官方 SDK 的 Start() 方法本身没有内置超时机制
	// 建议设置为 10-30 秒，避免启动时长时间阻塞
	StartTimeout time.Duration
}

// NewSimpleConsumer 创建新的简单消费者
// conf: 消费者配置
//   - StartTimeout: Start() 方法的超时时间，如果为 0 则使用默认值 10 秒
//
// 返回: Consumer 实例和错误
// 使用示例：
//
//	// 使用默认超时（10秒）
//	consumer, err := NewSimpleConsumer(&ConsumerConfig{
//	    Group:     "your-group",
//	    Endpoint:  "your-endpoint",
//	    AccessKey: "your-key",
//	    SecretKey: "your-secret",
//	    Topic:     "your-topic",
//	})
//
//	// 自定义超时时间
//	consumer, err := NewSimpleConsumer(&ConsumerConfig{
//	    Group:        "your-group",
//	    Endpoint:     "your-endpoint",
//	    AccessKey:    "your-key",
//	    SecretKey:    "your-secret",
//	    Topic:        "your-topic",
//	    StartTimeout: 30 * time.Second, // 自定义启动超时为 30 秒
//	})
func NewSimpleConsumer(conf *ConsumerConfig) (Consumer, error) {
	if conf == nil {
		return nil, errors.New("ConsumerConfig 不能为 nil")
	}
	if conf.Group == "" {
		return nil, errors.New("ConsumerGroup 不能为空")
	}
	if conf.Endpoint == "" {
		return nil, errors.New("Endpoint 不能为空")
	}

	consumer := &consumerEntity{ConsumerConfig: conf}
	await := util.If(conf.AwaitDuration > 0, conf.AwaitDuration, DefaultConsumerAwaitDuration)

	var subscriptionExpressions map[string]*FilterExpression
	if len(conf.SubscriptionExpressions) > 0 {
		// 优先使用配置的订阅表达式
		subscriptionExpressions = conf.SubscriptionExpressions
	} else if conf.Topic != "" {
		// 使用 Topic 创建订阅表达式
		subscriptionExpressions = map[string]*FilterExpression{conf.Topic: SubAll}
	} else {
		return nil, errors.New("未设置订阅 topic 或 SubscriptionExpressions")
	}

	var err error
	consumer.simpleConsumer, err = rmqClient.NewSimpleConsumer(&rmqClient.Config{
		Endpoint:      conf.Endpoint,
		ConsumerGroup: conf.Group,
		Credentials: &credentials.SessionCredentials{
			AccessKey:    conf.AccessKey,
			AccessSecret: conf.SecretKey,
		},
	},
		rmqClient.WithSimpleAwaitDuration(await),
		rmqClient.WithSimpleSubscriptionExpressions(subscriptionExpressions),
	)
	if err != nil {
		return nil, err
	}
	return consumer, nil
}

// Start 启动消费者并建立网络连接（可能阻塞）
// 注意：
// 1. 官方 SDK 的 Start() 方法本身没有内置超时机制，可能会无限阻塞
// 2. 默认使用 DefaultConsumerStartTimeout (10秒) 作为超时时间
// 3. 如果配置了 StartTimeout，则使用配置的值
// 4. 如果 StartTimeout 为 0，则使用默认值 10 秒
//
// 超时控制说明：
// - 这是应用层的超时包装，用于防止 Start() 在网络不通时无限阻塞
// - SDK 内部可能有连接重试机制，但 Start() 方法本身不提供超时参数
// - 如果需要在 SDK 层面控制连接超时，可以通过 WithClientConnFunc 和 WithDialTimeout 选项
func (c *consumerEntity) Start() error {
	// 确定超时时间：如果未设置或为 0，使用默认值
	timeout := c.StartTimeout
	if timeout <= 0 {
		timeout = DefaultConsumerStartTimeout
	}

	// 应用层超时控制：使用 goroutine + channel 实现
	// 注意：这不会取消 SDK 内部的连接操作，只是在外层设置超时
	done := make(chan error, 1)
	go func() {
		done <- c.simpleConsumer.Start()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		// 注意：超时后，SDK 内部的连接操作可能仍在进行
		// 如果需要，可以调用 Close() 来清理资源
		return fmt.Errorf("启动 Consumer 超时（超时时间: %v），可能网络不通或服务器不可达", timeout)
	}
}

// Receive 接收消息
// 使用默认的 context.Background()，如果需要超时控制，请使用 ReceiveWithContext
func (c *consumerEntity) Receive() ([]*MessageView, error) {
	return c.ReceiveWithContext(context.Background())
}

// ReceiveWithContext 接收消息（支持 context 控制）
func (c *consumerEntity) ReceiveWithContext(ctx context.Context) ([]*MessageView, error) {
	maxMessageNum := util.If(c.MaxMessageNum > 0, c.MaxMessageNum, DefaultConsumerMaxMessageNum)
	invisibleDuration := util.If(c.InvisibleDuration > 0, c.InvisibleDuration, DefaultConsumerInvisibleDuration)
	return c.simpleConsumer.Receive(ctx, maxMessageNum, invisibleDuration)
}

// GetSimpleConsumer 获取底层的 SimpleConsumer 实例
func (c *consumerEntity) GetSimpleConsumer() SimpleConsumer {
	return c.simpleConsumer
}

// Ack 确认消息已处理
func (c *consumerEntity) Ack(ctx context.Context, messageView *MessageView) error {
	return c.simpleConsumer.Ack(ctx, messageView)
}

// Close 关闭消费者并释放资源
func (c *consumerEntity) Close() error {
	return c.simpleConsumer.GracefulStop()
}
