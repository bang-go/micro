package rmq

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	rmqClient "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/prometheus/client_golang/prometheus"
)

func TestNewSimpleConsumerValidation(t *testing.T) {
	_, err := NewSimpleConsumer(nil)
	if !errors.Is(err, ErrNilConsumerConfig) {
		t.Fatalf("NewSimpleConsumer(nil) error = %v, want %v", err, ErrNilConsumerConfig)
	}

	_, err = NewSimpleConsumer(&ConsumerConfig{})
	if !errors.Is(err, ErrConsumerGroupEmpty) {
		t.Fatalf("NewSimpleConsumer(empty) error = %v, want %v", err, ErrConsumerGroupEmpty)
	}
}

func TestPrepareConsumerConfigNormalizesAndClonesInput(t *testing.T) {
	conf := &ConsumerConfig{
		Name:      " jobs ",
		Topic:     " job.created ",
		Group:     " jobs-group ",
		Endpoint:  " 127.0.0.1:8081 ",
		Namespace: " ns ",
		AccessKey: " ak ",
		SecretKey: " sk ",
		SubscriptionExpressions: map[string]*FilterExpression{
			" topic.a ": SubAll,
		},
	}

	normalized, sdkConfig, options, err := prepareConsumerConfig(conf)
	if err != nil {
		t.Fatalf("prepareConsumerConfig() error = %v", err)
	}

	if got, want := normalized.Name, "jobs"; got != want {
		t.Fatalf("Name = %q, want %q", got, want)
	}
	if got, want := normalized.Topic, "job.created"; got != want {
		t.Fatalf("Topic = %q, want %q", got, want)
	}
	if got, want := normalized.Group, "jobs-group"; got != want {
		t.Fatalf("Group = %q, want %q", got, want)
	}
	if got, want := normalized.Endpoint, "127.0.0.1:8081"; got != want {
		t.Fatalf("Endpoint = %q, want %q", got, want)
	}
	if got, want := sdkConfig.Credentials.AccessKey, "ak"; got != want {
		t.Fatalf("sdkConfig.Credentials.AccessKey = %q, want %q", got, want)
	}
	if got, want := sdkConfig.Credentials.AccessSecret, "sk"; got != want {
		t.Fatalf("sdkConfig.Credentials.AccessSecret = %q, want %q", got, want)
	}
	if got, want := len(normalized.SubscriptionExpressions), 1; got != want {
		t.Fatalf("len(SubscriptionExpressions) = %d, want %d", got, want)
	}
	if _, ok := normalized.SubscriptionExpressions["topic.a"]; !ok {
		t.Fatal("normalized subscriptions missing trimmed topic")
	}
	if got, want := len(options), 2; got != want {
		t.Fatalf("len(options) = %d, want %d", got, want)
	}

	conf.Name = "mutated"
	conf.Topic = "mutated"
	delete(conf.SubscriptionExpressions, " topic.a ")
	if got, want := normalized.Name, "jobs"; got != want {
		t.Fatalf("normalized.Name = %q, want %q", got, want)
	}
	if got, want := normalized.Topic, "job.created"; got != want {
		t.Fatalf("normalized.Topic = %q, want %q", got, want)
	}
	if _, ok := normalized.SubscriptionExpressions["topic.a"]; !ok {
		t.Fatal("normalized subscriptions mutated with input")
	}
}

func TestPrepareConsumerConfigRejectsDuplicateNormalizedSubscriptions(t *testing.T) {
	_, _, _, err := prepareConsumerConfig(&ConsumerConfig{
		Group:    "jobs-group",
		Endpoint: "127.0.0.1:8081",
		SubscriptionExpressions: map[string]*FilterExpression{
			"topic.a":   SubAll,
			" topic.a ": SubAll,
		},
	})
	if !errors.Is(err, ErrDuplicateSubscriptionKey) {
		t.Fatalf("prepareConsumerConfig() error = %v, want %v", err, ErrDuplicateSubscriptionKey)
	}
}

func TestSubscriptionsNameUsesFinalSubscriptionSet(t *testing.T) {
	got := subscriptionsName(map[string]*FilterExpression{
		"b": SubAll,
		"a": SubAll,
	})
	if want := "a,b"; got != want {
		t.Fatalf("subscriptionsName() = %q, want %q", got, want)
	}
}

func TestNewSimpleConsumerMetricsRegistererAndDisableMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	consumer, err := NewSimpleConsumer(&ConsumerConfig{
		Group:             "jobs-group",
		Endpoint:          "127.0.0.1:8081",
		Topic:             "job.created",
		MetricsRegisterer: reg,
		newConsumer: func(cfg *rmqClient.Config, opts ...rmqClient.SimpleConsumerOption) (consumerAPI, error) {
			return &fakeConsumer{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSimpleConsumer() error = %v", err)
	}
	if _, err := consumer.Receive(context.Background()); err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if !slices.Contains(gatherMetricNames(t, reg), "rmq_consumer_requests_total") {
		t.Fatal("custom registry missing rmq_consumer_requests_total")
	}

	disabledReg := prometheus.NewRegistry()
	consumer, err = NewSimpleConsumer(&ConsumerConfig{
		Group:             "jobs-group",
		Endpoint:          "127.0.0.1:8081",
		Topic:             "job.created",
		DisableMetrics:    true,
		MetricsRegisterer: disabledReg,
		newConsumer: func(cfg *rmqClient.Config, opts ...rmqClient.SimpleConsumerOption) (consumerAPI, error) {
			return &fakeConsumer{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSimpleConsumer(disable metrics) error = %v", err)
	}
	if _, err := consumer.Receive(context.Background()); err != nil {
		t.Fatalf("Receive(disable metrics) error = %v", err)
	}
	if slices.Contains(gatherMetricNames(t, disabledReg), "rmq_consumer_requests_total") {
		t.Fatal("disabled metrics still registered consumer metrics")
	}
}

func TestConsumerLifecycleAndDefaults(t *testing.T) {
	fake := &fakeConsumer{
		messages: []*MessageView{{}},
	}
	consumer, err := NewSimpleConsumer(&ConsumerConfig{
		Name:          "jobs",
		Group:         "jobs-group",
		Endpoint:      "127.0.0.1:8081",
		Topic:         "job.created",
		MaxMessageNum: -1,
		newConsumer: func(cfg *rmqClient.Config, opts ...rmqClient.SimpleConsumerOption) (consumerAPI, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSimpleConsumer() error = %v", err)
	}

	if err := consumer.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	messages, err := consumer.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Receive() messages = %d, want 1", len(messages))
	}
	if got, want := fake.lastReceiveMaxMessages, defaultReceiveMaxMessages; got != want {
		t.Fatalf("Receive() maxMessageNum = %d, want %d", got, want)
	}
	if got, want := fake.lastInvisibleDuration, defaultInvisibleDuration; got != want {
		t.Fatalf("Receive() invisibleDuration = %v, want %v", got, want)
	}

	if err := consumer.Ack(context.Background(), messages[0]); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !fake.closed {
		t.Fatal("Close() did not stop consumer")
	}
}

func TestConsumerContextValidationAndTimeout(t *testing.T) {
	fake := &fakeConsumer{startDelay: 50 * time.Millisecond}
	consumer, err := NewSimpleConsumer(&ConsumerConfig{
		Group:        "jobs-group",
		Endpoint:     "127.0.0.1:8081",
		Topic:        "job.created",
		StartTimeout: 10 * time.Millisecond,
		newConsumer: func(cfg *rmqClient.Config, opts ...rmqClient.SimpleConsumerOption) (consumerAPI, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSimpleConsumer() error = %v", err)
	}

	if err := consumer.Start(nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Start(nil) error = %v, want %v", err, ErrContextRequired)
	}
	if err := consumer.Start(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start(timeout) error = %v, want deadline exceeded", err)
	}
	if _, err := consumer.Receive(nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Receive(nil) error = %v, want %v", err, ErrContextRequired)
	}
	if err := consumer.Ack(nil, &MessageView{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Ack(nil ctx) error = %v, want %v", err, ErrContextRequired)
	}
	if err := consumer.Ack(context.Background(), nil); !errors.Is(err, ErrMessageViewNil) {
		t.Fatalf("Ack(nil message) error = %v, want %v", err, ErrMessageViewNil)
	}
}

type fakeConsumer struct {
	startDelay             time.Duration
	messages               []*MessageView
	lastReceiveMaxMessages int32
	lastInvisibleDuration  time.Duration
	closed                 bool
}

func (f *fakeConsumer) Start() error {
	if f.startDelay > 0 {
		time.Sleep(f.startDelay)
	}
	return nil
}

func (f *fakeConsumer) Receive(ctx context.Context, maxMessageNum int32, invisibleDuration time.Duration) ([]*MessageView, error) {
	f.lastReceiveMaxMessages = maxMessageNum
	f.lastInvisibleDuration = invisibleDuration
	return f.messages, nil
}

func (f *fakeConsumer) Ack(ctx context.Context, messageView *MessageView) error {
	return nil
}

func (f *fakeConsumer) GracefulStop() error {
	f.closed = true
	return nil
}
