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
	// 如果遇到连接问题，可以尝试增加到 30-60 秒
	DefaultProducerStartTimeout = time.Second * 30
	// DefaultDialTimeout 默认的 gRPC 连接超时时间
	// 如果遇到连接问题，可以尝试增加到 20-30 秒
	DefaultDialTimeout = time.Second * 20
)

type ProducerConfig struct {
	//Topic     string
	Endpoint  string
	AccessKey string
	SecretKey string
	// StartTimeout Start() 方法的超时时间
	// 如果为 0，则使用默认值 DefaultProducerStartTimeout (30秒)
	// 注意：这是应用层的超时控制，用于防止 Start() 方法无限阻塞
	// 官方 SDK 的 Start() 方法本身没有内置超时机制
	// 如果遇到连接问题，建议设置为 30-60 秒
	StartTimeout time.Duration
	// DialTimeout gRPC 连接建立的超时时间
	// 如果为 0，则使用默认值 DefaultDialTimeout (20秒)
	// 注意：这是 SDK 层面的连接超时，用于控制 gRPC Dial 操作的超时
	// 如果遇到连接问题，建议设置为 20-30 秒，应该小于或等于 StartTimeout
	DialTimeout time.Duration
	// MaxAttempts 消息发送的最大重试次数（默认 SDK 为 3）
	// 注意：此参数只用于消息发送重试，不用于 Start() 连接过程
	// Start() 的连接过程没有最大尝试次数限制，会无限重试
	// 因此仍需要 StartTimeout 来控制 Start() 的超时
	MaxAttempts int32
}

// NewProducer 创建新的生产者
// conf: 生产者配置
//   - StartTimeout: Start() 方法的超时时间，如果为 0 则使用默认值 30 秒
//   - DialTimeout: gRPC 连接建立的超时时间，如果为 0 则使用默认值 20 秒
//
// 返回: Producer 实例和错误
// 使用示例：
//
//	// 使用默认配置（超时 30/20 秒）
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
//	    DialTimeout:  15 * time.Second, // 自定义连接超时为 15 秒
//	})
func NewProducer(conf *ProducerConfig) (Producer, error) {
	if conf == nil {
		return nil, errors.New("ProducerConfig 不能为 nil")
	}
	if conf.Endpoint == "" {
		return nil, errors.New("endpoint 不能为空")
	}

	producer := &producerEntity{ProducerConfig: conf}
	var err error

	// 确定连接超时时间：如果未设置或为 0，使用默认值
	dialTimeout := conf.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = DefaultDialTimeout
	}

	// 准备 Producer 选项
	var opts []rmqClient.ProducerOption
	// 如果配置了 MaxAttempts，使用配置的值（默认 SDK 为 3）
	// 注意：MaxAttempts 只用于消息发送重试，不用于 Start() 连接
	if conf.MaxAttempts > 0 {
		opts = append(opts, rmqClient.WithMaxAttempts(conf.MaxAttempts))
	}

	// 设置连接超时：通过 WithClientFunc 创建自定义客户端，使用 WithConnOptions 设置连接超时
	// 同时设置 QueryRouteTimeout，用于查询路由信息的超时
	queryRouteTimeout := dialTimeout
	if queryRouteTimeout > time.Second*10 {
		queryRouteTimeout = time.Second * 10 // QueryRouteTimeout 不需要太长
	}
	opts = append(opts, rmqClient.WithClientFunc(func(config *rmqClient.Config, clientOpts ...rmqClient.ClientOption) (rmqClient.Client, error) {
		// 添加连接超时选项
		clientOpts = append(clientOpts, rmqClient.WithConnOptions(rmqClient.WithDialTimeout(dialTimeout)))
		// 添加路由查询超时选项
		clientOpts = append(clientOpts, rmqClient.WithQueryRouteTimeout(queryRouteTimeout))
		return rmqClient.NewClient(config, clientOpts...)
	}))

	// 创建 Producer 配置
	rmqConfig := &rmqClient.Config{
		Endpoint: conf.Endpoint,
		Credentials: &credentials.SessionCredentials{
			AccessKey:    conf.AccessKey,
			AccessSecret: conf.SecretKey,
		},
	}

	producer.producer, err = rmqClient.NewProducer(rmqConfig, opts...)
	if err != nil {
		// 返回详细的错误信息，包括配置信息（但不包含敏感信息）
		return nil, fmt.Errorf("创建 Producer 失败: %w (Endpoint: %s, DialTimeout: %v)", err, conf.Endpoint, dialTimeout)
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
// - DialTimeout 用于控制 SDK 层面的 gRPC 连接超时（默认 10 秒）
// - StartTimeout 用于控制应用层的整体启动超时（默认 10 秒）
// - 建议 DialTimeout <= StartTimeout，确保连接超时不会超过启动超时
func (p *producerEntity) Start() error {
	// 确定超时时间：如果未设置或为 0，使用默认值
	timeout := p.StartTimeout
	if timeout <= 0 {
		timeout = DefaultProducerStartTimeout
	}

	// 应用层超时控制：使用 goroutine + channel 实现
	// 注意：这不会取消 SDK 内部的连接操作，只是在外层设置超时
	done := make(chan error, 1)
	var startErr error
	go func() {
		startErr = p.GetProducer().Start()
		done <- startErr
	}()

	select {
	case err := <-done:
		// 如果 SDK 返回了错误，包装并返回详细的错误信息
		if err != nil {
			return fmt.Errorf("启动 Producer 失败: %w (Endpoint: %s)", err, p.Endpoint)
		}
		return nil
	case <-time.After(timeout):
		// 超时后，尝试获取 SDK 内部的错误（如果有）
		// 注意：超时后，SDK 内部的连接操作可能仍在进行
		// 如果需要，可以调用 Close() 来清理资源
		if startErr != nil {
			return fmt.Errorf("启动 Producer 超时（超时时间: %v），可能网络不通或服务器不可达 (Endpoint: %s)，SDK 错误: %w", timeout, p.Endpoint, startErr)
		}
		return fmt.Errorf("启动 Producer 超时（超时时间: %v），可能网络不通或服务器不可达 (Endpoint: %s)", timeout, p.Endpoint)
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
