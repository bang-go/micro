package kafka

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
)

const (
	defaultPollMaxRecords = 32
	defaultPollTimeout    = 5 * time.Second
)

type consumerAPI interface {
	PollRecords(context.Context, int) kgo.Fetches
	CommitRecords(context.Context, ...*kgo.Record) error
	AllowRebalance()
	Close()
}

type consumerFactory func(...kgo.Opt) (consumerAPI, error)

type ConsumerConfig struct {
	Name              string
	Brokers           []string
	Topic             string
	Group             string
	ClientID          string
	Username          string
	Password          string
	EnableTLS         bool
	PollMaxRecords    int
	PollTimeout       time.Duration
	BlockRebalance    bool
	Logger            *logger.Logger
	EnableLogger      bool
	DisableMetrics    bool
	MetricsRegisterer prometheus.Registerer

	newConsumer consumerFactory
}

type Consumer interface {
	Start(context.Context) error
	Poll(context.Context) ([]*MessageView, error)
	Commit(context.Context, ...*MessageView) error
	AllowRebalance()
	Close() error
}

type MessageView struct {
	Topic     string
	Partition int32
	Offset    int64
	Key       []byte
	Value     []byte
	Timestamp time.Time
	Headers   []Header

	record *kgo.Record
}

type Header struct {
	Key   string
	Value []byte
}

type consumerEntity struct {
	name           string
	topic          string
	group          string
	pollMaxRecords int
	pollTimeout    time.Duration
	logger         *logger.Logger
	enableLogger   bool
	metrics        *metrics
	consumer       consumerAPI
}

func NewConsumer(conf *ConsumerConfig) (Consumer, error) {
	config, opts, err := prepareConsumerConfig(conf)
	if err != nil {
		return nil, err
	}
	factory := config.newConsumer
	if factory == nil {
		factory = func(opts ...kgo.Opt) (consumerAPI, error) {
			return kgo.NewClient(opts...)
		}
	}

	var metrics *metrics
	if !config.DisableMetrics {
		metrics = defaultKafkaMetrics()
		if config.MetricsRegisterer != nil {
			metrics = newKafkaMetrics(config.MetricsRegisterer)
		}
	}

	consumer, err := factory(opts...)
	if err != nil {
		return nil, fmt.Errorf("kafka: create consumer failed: %w", err)
	}

	return &consumerEntity{
		name:           config.Name,
		topic:          config.Topic,
		group:          config.Group,
		pollMaxRecords: config.PollMaxRecords,
		pollTimeout:    config.PollTimeout,
		logger:         config.Logger,
		enableLogger:   config.EnableLogger,
		metrics:        metrics,
		consumer:       consumer,
	}, nil
}

func (c *consumerEntity) Start(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if c.enableLogger {
		c.logger.Info(ctx, "kafka consumer started", "name", c.name, "topic", c.topic, "group", c.group)
	}
	return nil
}

func (c *consumerEntity) Poll(ctx context.Context) ([]*MessageView, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}

	pollCtx := ctx
	cancel := func() {}
	if c.pollTimeout > 0 {
		pollCtx, cancel = context.WithTimeout(ctx, c.pollTimeout)
	}
	defer cancel()

	startedAt := time.Now()
	fetches := c.consumer.PollRecords(pollCtx, c.pollMaxRecords)
	var firstErr error
	fetches.EachError(func(topic string, partition int32, err error) {
		if firstErr == nil {
			firstErr = fmt.Errorf("kafka: fetch topic=%s partition=%d failed: %w", topic, partition, err)
		}
	})

	messages := make([]*MessageView, 0)
	fetches.EachRecord(func(record *kgo.Record) {
		messages = append(messages, newMessageView(record))
	})

	status := receiveStatus(firstErr, len(messages))
	duration := time.Since(startedAt)
	if c.metrics != nil {
		c.metrics.consumerRequestsTotal.WithLabelValues(c.name, "poll", status).Inc()
		c.metrics.consumerDuration.WithLabelValues(c.name, "poll", status).Observe(duration.Seconds())
		c.metrics.consumerMessagesTotal.WithLabelValues(c.name, status).Add(float64(len(messages)))
	}
	if c.enableLogger {
		fields := []any{
			"name", c.name,
			"topic", c.topic,
			"group", c.group,
			"message_count", len(messages),
			"status", status,
			"duration", duration,
		}
		if firstErr != nil {
			c.logger.Error(ctx, "kafka consumer poll failed", append(fields, "error", firstErr.Error())...)
		} else {
			c.logger.Debug(ctx, "kafka consumer poll completed", fields...)
		}
	}

	return messages, firstErr
}

func (c *consumerEntity) Commit(ctx context.Context, messages ...*MessageView) error {
	if ctx == nil {
		return ErrContextRequired
	}
	records := make([]*kgo.Record, 0, len(messages))
	for _, message := range messages {
		if message == nil || message.record == nil {
			return ErrMessageViewNil
		}
		records = append(records, message.record)
	}
	if len(records) == 0 {
		return nil
	}

	startedAt := time.Now()
	err := c.consumer.CommitRecords(ctx, records...)
	status := "success"
	if err != nil {
		status = "error"
	}
	duration := time.Since(startedAt)
	if c.metrics != nil {
		c.metrics.consumerRequestsTotal.WithLabelValues(c.name, "commit", status).Inc()
		c.metrics.consumerDuration.WithLabelValues(c.name, "commit", status).Observe(duration.Seconds())
	}
	if c.enableLogger {
		fields := []any{
			"name", c.name,
			"topic", c.topic,
			"group", c.group,
			"message_count", len(records),
			"status", status,
			"duration", duration,
		}
		if err != nil {
			c.logger.Error(ctx, "kafka consumer commit failed", append(fields, "error", err.Error())...)
		} else {
			c.logger.Debug(ctx, "kafka consumer commit completed", fields...)
		}
	}
	if err != nil {
		return fmt.Errorf("kafka: commit records failed: %w", err)
	}
	return nil
}

func (c *consumerEntity) AllowRebalance() {
	c.consumer.AllowRebalance()
}

func (c *consumerEntity) Close() error {
	c.consumer.Close()
	return nil
}

func prepareConsumerConfig(conf *ConsumerConfig) (*ConsumerConfig, []kgo.Opt, error) {
	if conf == nil {
		return nil, nil, ErrNilConsumerConfig
	}
	cloned := *conf
	cloned.Name = strings.TrimSpace(cloned.Name)
	cloned.Topic = strings.TrimSpace(cloned.Topic)
	cloned.Group = strings.TrimSpace(cloned.Group)
	cloned.ClientID = strings.TrimSpace(cloned.ClientID)
	cloned.Username = strings.TrimSpace(cloned.Username)
	cloned.Password = strings.TrimSpace(cloned.Password)
	cloned.Logger = defaultLogger(cloned.Logger)
	cloned.Brokers = normalizeBrokers(cloned.Brokers)
	if len(cloned.Brokers) == 0 {
		return nil, nil, ErrBrokersRequired
	}
	if cloned.Topic == "" {
		return nil, nil, ErrTopicRequired
	}
	if cloned.Group == "" {
		return nil, nil, ErrConsumerGroupEmpty
	}
	if cloned.Name == "" {
		cloned.Name = cloned.Group
	}
	if cloned.ClientID == "" {
		cloned.ClientID = cloned.Name
	}
	if cloned.PollMaxRecords <= 0 {
		cloned.PollMaxRecords = defaultPollMaxRecords
	}
	if cloned.PollTimeout <= 0 {
		cloned.PollTimeout = defaultPollTimeout
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(cloned.Brokers...),
		kgo.ClientID(cloned.ClientID),
		kgo.ConsumerGroup(cloned.Group),
		kgo.ConsumeTopics(cloned.Topic),
		kgo.DisableAutoCommit(),
	}
	if cloned.BlockRebalance {
		opts = append(opts, kgo.BlockRebalanceOnPoll())
	}
	if cloned.Username != "" || cloned.Password != "" {
		opts = append(opts, kgo.SASL(plain.Auth{User: cloned.Username, Pass: cloned.Password}.AsMechanism()))
	}
	if cloned.EnableTLS {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	}
	return &cloned, opts, nil
}

func normalizeBrokers(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func receiveStatus(err error, count int) string {
	if err != nil {
		return "error"
	}
	if count == 0 {
		return "empty"
	}
	return "success"
}

func newMessageView(record *kgo.Record) *MessageView {
	headers := make([]Header, 0, len(record.Headers))
	for _, header := range record.Headers {
		headers = append(headers, Header{Key: header.Key, Value: header.Value})
	}
	return &MessageView{
		Topic:     record.Topic,
		Partition: record.Partition,
		Offset:    record.Offset,
		Key:       record.Key,
		Value:     record.Value,
		Timestamp: record.Timestamp,
		Headers:   headers,
		record:    record,
	}
}
