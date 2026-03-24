package rmq

import (
	"context"
	"fmt"
	"strings"
	"time"

	rmqClient "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/apache/rocketmq-clients/golang/v5/credentials"
	v2 "github.com/apache/rocketmq-clients/golang/v5/protocol/v2"
	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	CodeMessageNotFound = v2.Code_MESSAGE_NOT_FOUND
)

type consumerAPI interface {
	Start() error
	Receive(context.Context, int32, time.Duration) ([]*MessageView, error)
	Ack(context.Context, *MessageView) error
	GracefulStop() error
}

type consumerFactory func(*rmqClient.Config, ...rmqClient.SimpleConsumerOption) (consumerAPI, error)

type ConsumerConfig struct {
	Name                    string
	Topic                   string
	Group                   string
	Endpoint                string
	Namespace               string
	AccessKey               string
	SecretKey               string
	SubscriptionExpressions map[string]*FilterExpression
	AwaitDuration           time.Duration
	MaxMessageNum           int32
	InvisibleDuration       time.Duration
	StartTimeout            time.Duration

	Logger            *logger.Logger
	EnableLogger      bool
	DisableMetrics    bool
	MetricsRegisterer prometheus.Registerer

	newConsumer consumerFactory
}

type Consumer interface {
	Start(context.Context) error
	Receive(context.Context) ([]*MessageView, error)
	Ack(context.Context, *MessageView) error
	Close() error
}

type consumerEntity struct {
	name              string
	group             string
	subscriptionLabel string
	maxMessages       int32
	invisibleDuration time.Duration
	startTimeout      time.Duration
	logger            *logger.Logger
	enableLogger      bool
	metrics           *metrics
	consumer          consumerAPI
}

func NewSimpleConsumer(conf *ConsumerConfig) (Consumer, error) {
	config, sdkConfig, options, err := prepareConsumerConfig(conf)
	if err != nil {
		return nil, err
	}

	factory := config.newConsumer
	if factory == nil {
		factory = func(cfg *rmqClient.Config, opts ...rmqClient.SimpleConsumerOption) (consumerAPI, error) {
			return rmqClient.NewSimpleConsumer(cfg, opts...)
		}
	}

	var metrics *metrics
	if !config.DisableMetrics {
		metrics = defaultRMQMetrics()
		if config.MetricsRegisterer != nil {
			metrics = newRMQMetrics(config.MetricsRegisterer)
		}
	}

	consumer, err := factory(sdkConfig, options...)
	if err != nil {
		return nil, fmt.Errorf("rmq: create consumer failed: %w", err)
	}

	return &consumerEntity{
		name:              config.Name,
		group:             config.Group,
		subscriptionLabel: subscriptionsName(config.SubscriptionExpressions),
		maxMessages:       boundedMaxMessages(config.MaxMessageNum),
		invisibleDuration: invisibleDurationOrDefault(config.InvisibleDuration),
		startTimeout:      config.StartTimeout,
		logger:            config.Logger,
		enableLogger:      config.EnableLogger,
		metrics:           metrics,
		consumer:          consumer,
	}, nil
}

func (c *consumerEntity) Start(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}

	startCtx, cancel := timeoutContext(ctx, c.startTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- c.consumer.Start()
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("rmq: start consumer failed: %w", err)
		}
		if c.enableLogger {
			c.logger.Info(ctx, "rmq consumer started", "name", c.name, "group", c.group, "subscriptions", c.subscriptionLabel)
		}
		return nil
	case <-startCtx.Done():
		return fmt.Errorf("rmq: start consumer failed: %w", startCtx.Err())
	}
}

func (c *consumerEntity) Receive(ctx context.Context) ([]*MessageView, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}

	startedAt := time.Now()
	messages, err := c.consumer.Receive(ctx, c.maxMessages, c.invisibleDuration)
	status := receiveStatus(err)
	duration := time.Since(startedAt)

	if c.metrics != nil {
		c.metrics.consumerRequestsTotal.WithLabelValues(c.name, "receive", status).Inc()
		c.metrics.consumerDuration.WithLabelValues(c.name, "receive", status).Observe(duration.Seconds())
		c.metrics.consumerMessagesTotal.WithLabelValues(c.name, status).Add(float64(len(messages)))
	}

	if c.enableLogger {
		fields := []any{
			"name", c.name,
			"group", c.group,
			"subscriptions", c.subscriptionLabel,
			"message_count", len(messages),
			"status", status,
			"duration", duration,
		}
		switch status {
		case "error":
			c.logger.Error(ctx, "rmq consumer receive failed", append(fields, "error", err)...)
		case "empty":
			c.logger.Debug(ctx, "rmq consumer receive empty", fields...)
		default:
			c.logger.Debug(ctx, "rmq consumer receive completed", fields...)
		}
	}

	return messages, err
}

func (c *consumerEntity) Ack(ctx context.Context, messageView *MessageView) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if messageView == nil {
		return ErrMessageViewNil
	}

	startedAt := time.Now()
	err := c.consumer.Ack(ctx, messageView)
	status := "success"
	if err != nil {
		status = "error"
	}
	duration := time.Since(startedAt)

	if c.metrics != nil {
		c.metrics.consumerRequestsTotal.WithLabelValues(c.name, "ack", status).Inc()
		c.metrics.consumerDuration.WithLabelValues(c.name, "ack", status).Observe(duration.Seconds())
	}

	if c.enableLogger {
		fields := []any{
			"name", c.name,
			"group", c.group,
			"message_id", messageView.GetMessageId(),
			"status", status,
			"duration", duration,
		}
		if err != nil {
			c.logger.Error(ctx, "rmq consumer ack failed", append(fields, "error", err)...)
		} else {
			c.logger.Debug(ctx, "rmq consumer ack completed", fields...)
		}
	}

	return err
}

func (c *consumerEntity) Close() error {
	return c.consumer.GracefulStop()
}

func prepareConsumerConfig(conf *ConsumerConfig) (*ConsumerConfig, *rmqClient.Config, []rmqClient.SimpleConsumerOption, error) {
	if conf == nil {
		return nil, nil, nil, ErrNilConsumerConfig
	}

	cloned := *conf
	cloned.Name = strings.TrimSpace(cloned.Name)
	cloned.Topic = strings.TrimSpace(cloned.Topic)
	cloned.Group = strings.TrimSpace(cloned.Group)
	cloned.Endpoint = strings.TrimSpace(cloned.Endpoint)
	cloned.Namespace = strings.TrimSpace(cloned.Namespace)
	cloned.AccessKey = strings.TrimSpace(cloned.AccessKey)
	cloned.SecretKey = strings.TrimSpace(cloned.SecretKey)
	cloned.Logger = defaultLogger(cloned.Logger)
	if cloned.Group == "" {
		return nil, nil, nil, ErrConsumerGroupEmpty
	}
	if cloned.Endpoint == "" {
		return nil, nil, nil, ErrEndpointRequired
	}
	if cloned.StartTimeout <= 0 {
		cloned.StartTimeout = defaultStartTimeout
	}
	if cloned.AwaitDuration <= 0 {
		cloned.AwaitDuration = defaultReceiveAwaitDuration
	}
	if cloned.Name == "" {
		cloned.Name = cloned.Group
	}

	subscriptions, err := cloneSubscriptions(cloned.SubscriptionExpressions)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(subscriptions) == 0 {
		if cloned.Topic == "" {
			return nil, nil, nil, ErrSubscriptionMiss
		}
		subscriptions = map[string]*FilterExpression{cloned.Topic: SubAll}
	}
	cloned.SubscriptionExpressions = subscriptions

	sdkConfig := &rmqClient.Config{
		Endpoint:      cloned.Endpoint,
		NameSpace:     cloned.Namespace,
		ConsumerGroup: cloned.Group,
		Credentials:   &credentials.SessionCredentials{AccessKey: cloned.AccessKey, AccessSecret: cloned.SecretKey},
	}

	options := []rmqClient.SimpleConsumerOption{
		rmqClient.WithAwaitDuration(cloned.AwaitDuration),
		rmqClient.WithSubscriptionExpressions(subscriptions),
	}
	return &cloned, sdkConfig, options, nil
}

func boundedMaxMessages(value int32) int32 {
	if value <= 0 {
		return defaultReceiveMaxMessages
	}
	if value > maxReceiveMessages {
		return maxReceiveMessages
	}
	return value
}

func invisibleDurationOrDefault(value time.Duration) time.Duration {
	if value <= 0 {
		return defaultInvisibleDuration
	}
	return value
}

func receiveStatus(err error) string {
	if err == nil {
		return "success"
	}
	if rpcErr, ok := AsErrRpcStatus(err); ok && rpcErr.Code == int32(CodeMessageNotFound) {
		return "empty"
	}
	return "error"
}
