package rmq

import (
	"context"
	"fmt"
	"strings"
	"time"

	rmqClient "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/apache/rocketmq-clients/golang/v5/credentials"
	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
)

type producerAPI interface {
	Send(context.Context, *Message) ([]*SendReceipt, error)
	SendAsync(context.Context, *Message, func(context.Context, []*SendReceipt, error))
	Start() error
	GracefulStop() error
}

type producerFactory func(*rmqClient.Config, ...rmqClient.ProducerOption) (producerAPI, error)

type ProducerConfig struct {
	Name      string
	Endpoint  string
	Namespace string
	AccessKey string
	SecretKey string

	StartTimeout time.Duration
	DialTimeout  time.Duration
	MaxAttempts  int32

	Logger            *logger.Logger
	EnableLogger      bool
	DisableMetrics    bool
	MetricsRegisterer prometheus.Registerer

	newProducer producerFactory
}

type Producer interface {
	Start(context.Context) error
	Close() error
	Send(context.Context, *Message) ([]*SendReceipt, error)
	SendAsync(context.Context, *Message, AsyncSendHandler)
	SendFIFO(context.Context, *Message, string) ([]*SendReceipt, error)
	SendDelay(context.Context, *Message, time.Time) ([]*SendReceipt, error)
}

type producerEntity struct {
	name         string
	endpoint     string
	logger       *logger.Logger
	enableLogger bool
	startTimeout time.Duration
	metrics      *metrics
	producer     producerAPI
}

func NewProducer(conf *ProducerConfig) (Producer, error) {
	config, sdkConfig, options, err := prepareProducerConfig(conf)
	if err != nil {
		return nil, err
	}

	factory := config.newProducer
	if factory == nil {
		factory = func(cfg *rmqClient.Config, opts ...rmqClient.ProducerOption) (producerAPI, error) {
			return rmqClient.NewProducer(cfg, opts...)
		}
	}

	var metrics *metrics
	if !config.DisableMetrics {
		metrics = defaultRMQMetrics()
		if config.MetricsRegisterer != nil {
			metrics = newRMQMetrics(config.MetricsRegisterer)
		}
	}

	producer, err := factory(sdkConfig, options...)
	if err != nil {
		return nil, fmt.Errorf("rmq: create producer failed: %w", err)
	}

	return &producerEntity{
		name:         config.Name,
		endpoint:     config.Endpoint,
		logger:       config.Logger,
		enableLogger: config.EnableLogger,
		startTimeout: config.StartTimeout,
		metrics:      metrics,
		producer:     producer,
	}, nil
}

func (p *producerEntity) Start(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}

	startCtx, cancel := timeoutContext(ctx, p.startTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- p.producer.Start()
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("rmq: start producer failed: %w", err)
		}
		if p.enableLogger {
			p.logger.Info(ctx, "rmq producer started", "name", p.name, "endpoint", p.endpoint)
		}
		return nil
	case <-startCtx.Done():
		return fmt.Errorf("rmq: start producer failed: %w", startCtx.Err())
	}
}

func (p *producerEntity) Close() error {
	return p.producer.GracefulStop()
}

func (p *producerEntity) Send(ctx context.Context, message *Message) ([]*SendReceipt, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}

	prepared, err := prepareMessage(message)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	receipts, err := p.producer.Send(ctx, prepared)
	p.observeSend(ctx, "send", start, len(receipts), err)
	return receipts, err
}

func (p *producerEntity) SendAsync(ctx context.Context, message *Message, handler AsyncSendHandler) {
	if ctx == nil {
		if handler != nil {
			handler(context.Background(), nil, ErrContextRequired)
		}
		return
	}

	prepared, err := prepareMessage(message)
	if err != nil {
		if handler != nil {
			handler(ctx, nil, err)
		}
		return
	}

	start := time.Now()
	p.producer.SendAsync(ctx, prepared, func(ctx context.Context, receipts []*SendReceipt, err error) {
		p.observeSend(ctx, "send_async", start, len(receipts), err)
		if handler != nil {
			handler(ctx, receipts, err)
		}
	})
}

func (p *producerEntity) SendFIFO(ctx context.Context, message *Message, group string) ([]*SendReceipt, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}

	prepared, err := prepareMessage(message)
	if err != nil {
		return nil, err
	}
	group = strings.TrimSpace(group)
	if group == "" {
		return nil, ErrMessageGroupEmpty
	}
	prepared.SetMessageGroup(group)

	start := time.Now()
	receipts, err := p.producer.Send(ctx, prepared)
	p.observeSend(ctx, "send_fifo", start, len(receipts), err)
	return receipts, err
}

func (p *producerEntity) SendDelay(ctx context.Context, message *Message, deliverAt time.Time) ([]*SendReceipt, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}

	prepared, err := prepareMessage(message)
	if err != nil {
		return nil, err
	}
	prepared.SetDelayTimestamp(deliverAt)

	start := time.Now()
	receipts, err := p.producer.Send(ctx, prepared)
	p.observeSend(ctx, "send_delay", start, len(receipts), err)
	return receipts, err
}

func (p *producerEntity) observeSend(ctx context.Context, operation string, startedAt time.Time, receiptCount int, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	duration := time.Since(startedAt)

	if p.metrics != nil {
		p.metrics.producerRequestsTotal.WithLabelValues(p.name, operation, status).Inc()
		p.metrics.producerDuration.WithLabelValues(p.name, operation, status).Observe(duration.Seconds())
	}

	if !p.enableLogger {
		return
	}
	fields := []any{
		"name", p.name,
		"endpoint", p.endpoint,
		"operation", operation,
		"receipt_count", receiptCount,
		"duration", duration,
	}
	if err != nil {
		p.logger.Error(ctx, "rmq producer request failed", append(fields, "error", err)...)
		return
	}
	p.logger.Debug(ctx, "rmq producer request completed", fields...)
}

func prepareProducerConfig(conf *ProducerConfig) (*ProducerConfig, *rmqClient.Config, []rmqClient.ProducerOption, error) {
	if conf == nil {
		return nil, nil, nil, ErrNilProducerConfig
	}

	cloned := *conf
	cloned.Name = strings.TrimSpace(cloned.Name)
	cloned.Endpoint = strings.TrimSpace(cloned.Endpoint)
	cloned.Namespace = strings.TrimSpace(cloned.Namespace)
	cloned.AccessKey = strings.TrimSpace(cloned.AccessKey)
	cloned.SecretKey = strings.TrimSpace(cloned.SecretKey)
	cloned.Logger = defaultLogger(cloned.Logger)
	if cloned.Endpoint == "" {
		return nil, nil, nil, ErrEndpointRequired
	}
	if cloned.StartTimeout <= 0 {
		cloned.StartTimeout = defaultStartTimeout
	}
	if cloned.DialTimeout <= 0 {
		cloned.DialTimeout = defaultDialTimeout
	}
	if cloned.Name == "" {
		cloned.Name = cloned.Endpoint
	}

	sdkConfig := &rmqClient.Config{
		Endpoint:      cloned.Endpoint,
		NameSpace:     cloned.Namespace,
		Credentials:   &credentials.SessionCredentials{AccessKey: cloned.AccessKey, AccessSecret: cloned.SecretKey},
		ConsumerGroup: "",
	}

	queryRouteTimeout := cloned.DialTimeout
	if queryRouteTimeout > defaultQueryRouteTimeout {
		queryRouteTimeout = defaultQueryRouteTimeout
	}

	options := []rmqClient.ProducerOption{
		rmqClient.WithClientFunc(func(config *rmqClient.Config, clientOptions ...rmqClient.ClientOption) (rmqClient.Client, error) {
			clientOptions = append(clientOptions,
				rmqClient.WithConnOptions(rmqClient.WithDialTimeout(cloned.DialTimeout)),
				rmqClient.WithQueryRouteTimeout(queryRouteTimeout),
			)
			return rmqClient.NewClient(config, clientOptions...)
		}),
	}
	if cloned.MaxAttempts > 0 {
		options = append(options, rmqClient.WithMaxAttempts(cloned.MaxAttempts))
	}
	return &cloned, sdkConfig, options, nil
}

func prepareMessage(message *Message) (*Message, error) {
	switch {
	case message == nil:
		return nil, ErrMessageRequired
	case strings.TrimSpace(message.Topic) == "":
		return nil, ErrTopicRequired
	}

	return cloneMessage(message), nil
}
