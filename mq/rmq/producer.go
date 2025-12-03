package rmq

import (
	"context"
	"errors"
	"fmt"
	"time"

	rmqClient "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/apache/rocketmq-clients/golang/v5/credentials"
)

type RProducer = rmqClient.Producer
type Producer interface {
	Start() error
	Close() error
	GetProducer() RProducer
	// SendNormalMessage 同步发送普通消息（阻塞方法）
	// 注意：此方法会阻塞等待服务器响应，建议使用 context 设置超时时间
	// 如果需要非阻塞发送，请使用 AsyncSendNormalMessage
	SendNormalMessage(context.Context, *Message) ([]*SendReceipt, error)
	// AsyncSendNormalMessage 异步发送普通消息（非阻塞方法）
	// handler: 发送完成后的回调函数
	AsyncSendNormalMessage(context.Context, *Message, AsyncSendHandler)
	// SendFifoMessage 同步发送顺序消息（阻塞方法）
	// 注意：此方法会阻塞等待服务器响应，建议使用 context 设置超时时间
	SendFifoMessage(context.Context, *Message) ([]*SendReceipt, error)
	// SendDelayMessage 同步发送延时消息（阻塞方法）
	// 注意：此方法会阻塞等待服务器响应，建议使用 context 设置超时时间
	SendDelayMessage(context.Context, *Message, time.Time) ([]*SendReceipt, error)
}
type producerEntity struct {
	*ProducerConfig
	producer RProducer
}
type Message = rmqClient.Message
type SendReceipt = rmqClient.SendReceipt
type AsyncSendHandler = func(context.Context, []*SendReceipt, error)

const (
	// DefaultProducerStartTimeout 默认的 Producer Start() 超时时间
	DefaultProducerStartTimeout = time.Second * 10
)

type ProducerConfig struct {
	//Topic     string
	Endpoint  string
	AccessKey string
	SecretKey string
	// StartTimeout Start() 方法的超时时间
	// 如果为 0，则使用默认值 DefaultProducerStartTimeout (10秒)
	// 注意：这是应用层的超时控制，用于防止 Start() 方法无限阻塞
	// 官方 SDK 的 Start() 方法本身没有内置超时机制
	// 建议设置为 10-30 秒，避免启动时长时间阻塞
	StartTimeout time.Duration
	// MaxAttempts 消息发送的最大重试次数（默认 SDK 为 3）
	// 注意：此参数只用于消息发送重试，不用于 Start() 连接过程
	// Start() 的连接过程没有最大尝试次数限制，会无限重试
	// 因此仍需要 StartTimeout 来控制 Start() 的超时
	MaxAttempts int32
}

// NewProducer 创建新的生产者
// conf: 生产者配置
//   - StartTimeout: Start() 方法的超时时间，如果为 0 则使用默认值 10 秒
//
// 返回: Producer 实例和错误
// 使用示例：
//
//	// 使用默认超时（10秒）
//	producer, err := NewProducer(&ProducerConfig{
//	    Endpoint:  "your-endpoint",
//	    AccessKey: "your-key",
//	    SecretKey: "your-secret",
//	})
//
//	// 自定义超时时间
//	producer, err := NewProducer(&ProducerConfig{
//	    Endpoint:     "your-endpoint",
//	    AccessKey:    "your-key",
//	    SecretKey:    "your-secret",
//	    StartTimeout: 30 * time.Second, // 自定义启动超时为 30 秒
//	})
func NewProducer(conf *ProducerConfig) (Producer, error) {
	if conf == nil {
		return nil, errors.New("ProducerConfig 不能为 nil")
	}
	if conf.Endpoint == "" {
		return nil, errors.New("Endpoint 不能为空")
	}

	producer := &producerEntity{ProducerConfig: conf}
	var err error

	// 准备 Producer 选项
	var opts []rmqClient.ProducerOption
	// 如果配置了 MaxAttempts，使用配置的值（默认 SDK 为 3）
	// 注意：MaxAttempts 只用于消息发送重试，不用于 Start() 连接
	if conf.MaxAttempts > 0 {
		opts = append(opts, rmqClient.WithMaxAttempts(conf.MaxAttempts))
	}

	producer.producer, err = rmqClient.NewProducer(&rmqClient.Config{
		Endpoint: conf.Endpoint,
		Credentials: &credentials.SessionCredentials{
			AccessKey:    conf.AccessKey,
			AccessSecret: conf.SecretKey,
		},
	}, opts...)
	if err != nil {
		return nil, err
	}
	return producer, nil
}

// Start 启动生产者并建立网络连接（可能阻塞）
// 注意：
// 1. 官方 SDK 的 Start() 方法本身没有内置超时机制，可能会无限阻塞
// 2. 默认使用 DefaultProducerStartTimeout (10秒) 作为超时时间
// 3. 如果配置了 StartTimeout，则使用配置的值
// 4. 如果 StartTimeout 为 0，则使用默认值 10 秒
//
// 关于 MaxAttempts 和 StartTimeout 的区别：
// - MaxAttempts: 只用于消息发送的重试次数，不用于 Start() 连接过程
// - StartTimeout: 用于控制 Start() 方法的超时，防止连接过程无限阻塞
// - Start() 的连接过程没有最大尝试次数限制，会无限重试，因此需要超时控制
//
// 超时控制说明：
// - 这是应用层的超时包装，用于防止 Start() 在网络不通时无限阻塞
// - SDK 内部可能有连接重试机制，但 Start() 方法本身不提供超时参数
// - 如果需要在 SDK 层面控制连接超时，可以通过 WithClientConnFunc 和 WithDialTimeout 选项
func (p *producerEntity) Start() error {
	// 确定超时时间：如果未设置或为 0，使用默认值
	timeout := p.StartTimeout
	if timeout <= 0 {
		timeout = DefaultProducerStartTimeout
	}

	// 应用层超时控制：使用 goroutine + channel 实现
	// 注意：这不会取消 SDK 内部的连接操作，只是在外层设置超时
	done := make(chan error, 1)
	go func() {
		done <- p.GetProducer().Start()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		// 注意：超时后，SDK 内部的连接操作可能仍在进行
		// 如果需要，可以调用 Close() 来清理资源
		return fmt.Errorf("启动 Producer 超时（超时时间: %v），可能网络不通或服务器不可达", timeout)
	}
}

// Close 关闭生产者并释放资源
func (p *producerEntity) Close() error {
	return p.GetProducer().GracefulStop()
}

// GetProducer 获取底层的 Producer 实例
func (p *producerEntity) GetProducer() RProducer {
	return p.producer
}

// SendNormalMessage 同步发送普通消息
// 注意：此方法是阻塞的，会等待服务器响应后才返回
// 建议使用 context.WithTimeout 设置超时时间，避免长时间阻塞
// 示例：
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	receipt, err := producer.SendNormalMessage(ctx, msg)
func (p *producerEntity) SendNormalMessage(ctx context.Context, msg *Message) ([]*SendReceipt, error) {
	return p.GetProducer().Send(ctx, msg)
}

// AsyncSendNormalMessage 异步发送普通消息（非阻塞）
// 此方法不会阻塞，消息发送完成后会通过 handler 回调通知结果
// handler: 回调函数，参数为 context、发送回执列表和错误
// 示例：
//
//	producer.AsyncSendNormalMessage(ctx, msg, func(ctx context.Context, receipts []*SendReceipt, err error) {
//	    if err != nil {
//	        log.Printf("发送失败: %v", err)
//	        return
//	    }
//	    log.Printf("发送成功: %v", receipts[0].MessageID)
//	})
func (p *producerEntity) AsyncSendNormalMessage(ctx context.Context, msg *Message, handler AsyncSendHandler) {
	p.GetProducer().SendAsync(ctx, msg, handler)
}

// SendFifoMessage 同步发送顺序消息（阻塞方法）
// 注意：此方法会阻塞等待服务器响应，建议使用 context 设置超时时间
// 消息会被设置到 "fifo" 消息组，保证同一组的消息顺序消费
func (p *producerEntity) SendFifoMessage(ctx context.Context, msg *Message) ([]*SendReceipt, error) {
	msg.SetMessageGroup("fifo")
	return p.GetProducer().Send(ctx, msg)
}

// SendDelayMessage 同步发送延时消息（阻塞方法）
// 注意：此方法会阻塞等待服务器响应，建议使用 context 设置超时时间
// delayTimestamp: 消息的延迟投递时间
func (p *producerEntity) SendDelayMessage(ctx context.Context, msg *Message, delayTimestamp time.Time) ([]*SendReceipt, error) {
	msg.SetDelayTimestamp(delayTimestamp)
	return p.GetProducer().Send(ctx, msg)
}
