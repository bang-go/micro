package rmq

import (
	"context"
	"errors"
	"fmt"
	"time"

	rmqClient "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/apache/rocketmq-clients/golang/v5/credentials"
	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics
var (
	ProducerMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rmq_producer_messages_total",
			Help: "Total number of messages sent by producer",
		},
		[]string{"endpoint", "status"},
	)

	ProducerDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rmq_producer_duration_seconds",
			Help:    "Duration of message sending in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"endpoint", "status"},
	)
)

func init() {
	prometheus.MustRegister(ProducerMessagesTotal)
	prometheus.MustRegister(ProducerDuration)
}

type RProducer = rmqClient.Producer
type Producer interface {
	Start() error
	Close() error
	GetProducer() RProducer
	SendNormalMessage(context.Context, *Message) ([]*SendReceipt, error)
	AsyncSendNormalMessage(context.Context, *Message, AsyncSendHandler)
	SendFifoMessage(context.Context, *Message) ([]*SendReceipt, error)
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
	StartTimeout time.Duration
	// DialTimeout gRPC 连接建立的超时时间
	DialTimeout time.Duration
	// MaxAttempts 消息发送的最大重试次数（默认 SDK 为 3）
	MaxAttempts int32

	Logger       *logger.Logger
	EnableLogger bool
}

// NewProducer creates a new producer
func NewProducer(conf *ProducerConfig) (Producer, error) {
	if conf == nil {
		return nil, errors.New("rmq: producer config is required")
	}
	if conf.Endpoint == "" {
		return nil, errors.New("rmq: endpoint is required")
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
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
	if conf.MaxAttempts > 0 {
		opts = append(opts, rmqClient.WithMaxAttempts(conf.MaxAttempts))
	}

	// 设置连接超时
	queryRouteTimeout := dialTimeout
	if queryRouteTimeout > time.Second*10 {
		queryRouteTimeout = time.Second * 10
	}
	opts = append(opts, rmqClient.WithClientFunc(func(config *rmqClient.Config, clientOpts ...rmqClient.ClientOption) (rmqClient.Client, error) {
		clientOpts = append(clientOpts, rmqClient.WithConnOptions(rmqClient.WithDialTimeout(dialTimeout)))
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
		return nil, fmt.Errorf("rmq: create producer failed: %w (Endpoint: %s, DialTimeout: %v)", err, conf.Endpoint, dialTimeout)
	}
	return producer, nil
}

// Start starts the producer
func (p *producerEntity) Start() error {
	timeout := p.StartTimeout
	if timeout <= 0 {
		timeout = DefaultProducerStartTimeout
	}

	done := make(chan error, 1)
	var startErr error
	go func() {
		startErr = p.GetProducer().Start()
		done <- startErr
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("rmq: start producer failed: %w (Endpoint: %s)", err, p.Endpoint)
		}
		if p.EnableLogger {
			p.Logger.Info(context.Background(), "rmq_producer_started", "endpoint", p.Endpoint)
		}
		return nil
	case <-time.After(timeout):
		if startErr != nil {
			return fmt.Errorf("rmq: start producer timeout (%v), SDK error: %w (Endpoint: %s)", timeout, startErr, p.Endpoint)
		}
		return fmt.Errorf("rmq: start producer timeout (%v) (Endpoint: %s)", timeout, p.Endpoint)
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

// recordMetrics helper
func (p *producerEntity) recordMetrics(ctx context.Context, start time.Time, err error) {
	duration := time.Since(start).Seconds()
	status := "success"
	if err != nil {
		status = "error"
	}
	ProducerMessagesTotal.WithLabelValues(p.Endpoint, status).Inc()
	ProducerDuration.WithLabelValues(p.Endpoint, status).Observe(duration)

	if p.EnableLogger {
		if err != nil {
			p.Logger.Error(ctx, "rmq_producer_send_failed", "endpoint", p.Endpoint, "error", err, "cost", duration)
		} else {
			p.Logger.Info(ctx, "rmq_producer_send_success", "endpoint", p.Endpoint, "cost", duration)
		}
	}
}

// SendNormalMessage 同步发送普通消息
func (p *producerEntity) SendNormalMessage(ctx context.Context, msg *Message) ([]*SendReceipt, error) {
	start := time.Now()
	receipts, err := p.GetProducer().Send(ctx, msg)
	p.recordMetrics(ctx, start, err)
	return receipts, err
}

// AsyncSendNormalMessage 异步发送普通消息
func (p *producerEntity) AsyncSendNormalMessage(ctx context.Context, msg *Message, handler AsyncSendHandler) {
	start := time.Now()
	p.GetProducer().SendAsync(ctx, msg, func(ctx context.Context, receipts []*SendReceipt, err error) {
		p.recordMetrics(ctx, start, err)
		if handler != nil {
			handler(ctx, receipts, err)
		}
	})
}

// SendFifoMessage 同步发送顺序消息
func (p *producerEntity) SendFifoMessage(ctx context.Context, msg *Message) ([]*SendReceipt, error) {
	msg.SetMessageGroup("fifo")
	return p.SendNormalMessage(ctx, msg)
}

// SendDelayMessage 同步发送延时消息
func (p *producerEntity) SendDelayMessage(ctx context.Context, msg *Message, delayTimestamp time.Time) ([]*SendReceipt, error) {
	msg.SetDelayTimestamp(delayTimestamp)
	return p.SendNormalMessage(ctx, msg)
}
