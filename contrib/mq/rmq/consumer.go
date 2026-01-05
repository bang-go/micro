package rmq

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	rmqClient "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/apache/rocketmq-clients/golang/v5/credentials"
	v2 "github.com/apache/rocketmq-clients/golang/v5/protocol/v2"
	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/util"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics
var (
	ConsumerMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rmq_consumer_messages_total",
			Help: "Total number of messages received by consumer",
		},
		[]string{"topic", "group", "status"},
	)
)

func init() {
	prometheus.MustRegister(ConsumerMessagesTotal)
}

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
	StartTimeout time.Duration

	Logger       *logger.Logger
	EnableLogger bool
}

// NewSimpleConsumer creates a new simple consumer
func NewSimpleConsumer(conf *ConsumerConfig) (Consumer, error) {
	if conf == nil {
		return nil, errors.New("rmq: consumer config is required")
	}
	if conf.Group == "" {
		return nil, errors.New("rmq: consumer group is required")
	}
	if conf.Endpoint == "" {
		return nil, errors.New("rmq: endpoint is required")
	}
	if conf.Logger == nil {
		conf.Logger = logger.New(logger.WithLevel("info"))
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
		return nil, errors.New("rmq: topic or subscription expressions are required")
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
		rmqClient.WithAwaitDuration(await),
		rmqClient.WithSubscriptionExpressions(subscriptionExpressions),
	)
	if err != nil {
		return nil, err
	}
	return consumer, nil
}

// Start starts the consumer
func (c *consumerEntity) Start() error {
	// 确定超时时间：如果未设置或为 0，使用默认值
	timeout := c.StartTimeout
	if timeout <= 0 {
		timeout = DefaultConsumerStartTimeout
	}

	// 应用层超时控制：使用 goroutine + channel 实现
	done := make(chan error, 1)
	go func() {
		done <- c.simpleConsumer.Start()
	}()

	select {
	case err := <-done:
		if err == nil && c.EnableLogger {
			topic := c.Topic
			if topic == "" && len(c.SubscriptionExpressions) > 0 {
				topics := make([]string, 0, len(c.SubscriptionExpressions))
				for t := range c.SubscriptionExpressions {
					topics = append(topics, t)
				}
				sort.Strings(topics)
				topic = strings.Join(topics, ",")
			}
			c.Logger.Info(context.Background(), "rmq_consumer_started", "group", c.Group, "topic", topic)
		}
		return err
	case <-time.After(timeout):
		// 注意：超时后，SDK 内部的连接操作可能仍在进行
		return fmt.Errorf("rmq: start consumer timeout (%v)", timeout)
	}
}

// Receive 接收消息
func (c *consumerEntity) Receive() ([]*MessageView, error) {
	return c.ReceiveWithContext(context.Background())
}

// ReceiveWithContext 接收消息（支持 context 控制）
func (c *consumerEntity) ReceiveWithContext(ctx context.Context) ([]*MessageView, error) {
	maxMessageNum := util.If(c.MaxMessageNum > 0, c.MaxMessageNum, DefaultConsumerMaxMessageNum)
	invisibleDuration := util.If(c.InvisibleDuration > 0, c.InvisibleDuration, DefaultConsumerInvisibleDuration)

	start := time.Now()
	msgs, err := c.simpleConsumer.Receive(ctx, maxMessageNum, invisibleDuration)
	duration := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		status = "error"
		// Ignore code: message not found (it's normal when polling)
		if asErr, ok := AsErrRpcStatus(err); ok && asErr.Code == int32(CodeMessageNotFound) {
			status = "empty"
		} else if c.EnableLogger {
			c.Logger.Error(ctx, "rmq_consumer_receive_failed",
				"group", c.Group,
				"error", err,
				"cost", duration,
			)
		}
	} else if c.EnableLogger && len(msgs) > 0 {
		// Optional: log received messages summary
		c.Logger.Info(ctx, "rmq_consumer_receive_success",
			"group", c.Group,
			"count", len(msgs),
			"cost", duration,
		)
	}

	// Metrics
	ConsumerMessagesTotal.WithLabelValues(c.Topic, c.Group, status).Add(float64(len(msgs)))

	return msgs, err
}

// GetSimpleConsumer 获取底层的 SimpleConsumer 实例
func (c *consumerEntity) GetSimpleConsumer() SimpleConsumer {
	return c.simpleConsumer
}

// Ack 确认消息已处理
func (c *consumerEntity) Ack(ctx context.Context, messageView *MessageView) error {
	err := c.simpleConsumer.Ack(ctx, messageView)
	if err != nil && c.EnableLogger {
		c.Logger.Error(ctx, "rmq_consumer_ack_failed",
			"group", c.Group,
			"msg_id", messageView.GetMessageId(),
			"error", err,
		)
	}
	return err
}

// Close 关闭消费者并释放资源
func (c *consumerEntity) Close() error {
	return c.simpleConsumer.GracefulStop()
}
