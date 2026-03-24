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

func TestNewProducerValidation(t *testing.T) {
	_, err := NewProducer(nil)
	if !errors.Is(err, ErrNilProducerConfig) {
		t.Fatalf("NewProducer(nil) error = %v, want %v", err, ErrNilProducerConfig)
	}

	_, err = NewProducer(&ProducerConfig{})
	if !errors.Is(err, ErrEndpointRequired) {
		t.Fatalf("NewProducer(empty) error = %v, want %v", err, ErrEndpointRequired)
	}
}

func TestPrepareProducerConfigNormalizesAndClonesInput(t *testing.T) {
	conf := &ProducerConfig{
		Name:        " payments ",
		Endpoint:    " 127.0.0.1:8081 ",
		Namespace:   " ns ",
		AccessKey:   " ak ",
		SecretKey:   " sk ",
		MaxAttempts: 2,
	}

	normalized, sdkConfig, _, err := prepareProducerConfig(conf)
	if err != nil {
		t.Fatalf("prepareProducerConfig() error = %v", err)
	}

	if got, want := normalized.Name, "payments"; got != want {
		t.Fatalf("Name = %q, want %q", got, want)
	}
	if got, want := normalized.Endpoint, "127.0.0.1:8081"; got != want {
		t.Fatalf("Endpoint = %q, want %q", got, want)
	}
	if got, want := sdkConfig.NameSpace, "ns"; got != want {
		t.Fatalf("sdkConfig.NameSpace = %q, want %q", got, want)
	}
	if got, want := sdkConfig.Credentials.AccessKey, "ak"; got != want {
		t.Fatalf("sdkConfig.Credentials.AccessKey = %q, want %q", got, want)
	}
	if got, want := sdkConfig.Credentials.AccessSecret, "sk"; got != want {
		t.Fatalf("sdkConfig.Credentials.AccessSecret = %q, want %q", got, want)
	}

	conf.Name = "mutated"
	conf.Endpoint = "mutated"
	conf.Namespace = "mutated"
	if got, want := normalized.Name, "payments"; got != want {
		t.Fatalf("normalized.Name = %q, want %q", got, want)
	}
	if got, want := normalized.Endpoint, "127.0.0.1:8081"; got != want {
		t.Fatalf("normalized.Endpoint = %q, want %q", got, want)
	}
}

func TestNewProducerMetricsRegistererAndDisableMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	producer, err := NewProducer(&ProducerConfig{
		Endpoint:          "127.0.0.1:8081",
		MetricsRegisterer: reg,
		newProducer: func(cfg *rmqClient.Config, opts ...rmqClient.ProducerOption) (producerAPI, error) {
			return &fakeProducer{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if _, err := producer.Send(context.Background(), &Message{Topic: "payment.created", Body: []byte("ok")}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !slices.Contains(gatherMetricNames(t, reg), "rmq_producer_requests_total") {
		t.Fatal("custom registry missing rmq_producer_requests_total")
	}

	disabledReg := prometheus.NewRegistry()
	producer, err = NewProducer(&ProducerConfig{
		Endpoint:          "127.0.0.1:8081",
		DisableMetrics:    true,
		MetricsRegisterer: disabledReg,
		newProducer: func(cfg *rmqClient.Config, opts ...rmqClient.ProducerOption) (producerAPI, error) {
			return &fakeProducer{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer(disable metrics) error = %v", err)
	}
	if _, err := producer.Send(context.Background(), &Message{Topic: "payment.created", Body: []byte("ok")}); err != nil {
		t.Fatalf("Send(disable metrics) error = %v", err)
	}
	if slices.Contains(gatherMetricNames(t, disabledReg), "rmq_producer_requests_total") {
		t.Fatal("disabled metrics still registered producer metrics")
	}
}

func TestProducerLifecycleAndMessageIsolation(t *testing.T) {
	fake := &fakeProducer{}
	producer, err := NewProducer(&ProducerConfig{
		Name:     "payments",
		Endpoint: "127.0.0.1:8081",
		newProducer: func(cfg *rmqClient.Config, opts ...rmqClient.ProducerOption) (producerAPI, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}

	if err := producer.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	msg := &Message{Topic: " payment.created ", Body: []byte("ok")}
	msg.SetTag("payment")
	msg.SetKeys("k1", "k2")

	receipts, err := producer.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("Send() receipts = %d, want 1", len(receipts))
	}
	if got, want := fake.lastSendMessage.Topic, "payment.created"; got != want {
		t.Fatalf("sent topic = %q, want %q", got, want)
	}
	if got, want := string(fake.lastSendMessage.Body), "ok"; got != want {
		t.Fatalf("sent body = %q, want %q", got, want)
	}
	if got, want := msg.Topic, " payment.created "; got != want {
		t.Fatalf("original topic mutated to %q, want %q", got, want)
	}

	msg.Body[0] = 'n'
	if got, want := string(fake.lastSendMessage.Body), "ok"; got != want {
		t.Fatalf("sent body mutated to %q, want %q", got, want)
	}

	done := make(chan struct{})
	producer.SendAsync(context.Background(), msg, func(ctx context.Context, receipts []*SendReceipt, err error) {
		defer close(done)
		if err != nil {
			t.Fatalf("SendAsync() error = %v", err)
		}
		if ctx == nil {
			t.Fatal("SendAsync() callback ctx = nil")
		}
	})
	<-done

	if _, err := producer.SendFIFO(context.Background(), msg, " group-1 "); err != nil {
		t.Fatalf("SendFIFO() error = %v", err)
	}
	group := fake.lastSendMessage.GetMessageGroup()
	if group == nil || *group != "group-1" {
		t.Fatalf("SendFIFO() message group = %v, want group-1", group)
	}
	if msg.GetMessageGroup() != nil {
		t.Fatal("SendFIFO() mutated original message group")
	}

	deliverAt := time.Now().Add(time.Minute).Round(time.Second)
	if _, err := producer.SendDelay(context.Background(), msg, deliverAt); err != nil {
		t.Fatalf("SendDelay() error = %v", err)
	}
	delay := fake.lastSendMessage.GetDeliveryTimestamp()
	if delay == nil || !delay.Equal(deliverAt) {
		t.Fatalf("SendDelay() delivery timestamp = %v, want %v", delay, deliverAt)
	}
	if msg.GetDeliveryTimestamp() != nil {
		t.Fatal("SendDelay() mutated original delivery timestamp")
	}

	if err := producer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !fake.closed {
		t.Fatal("Close() did not stop producer")
	}
}

func TestProducerContextValidationAndTimeout(t *testing.T) {
	fake := &fakeProducer{startDelay: 50 * time.Millisecond}
	producer, err := NewProducer(&ProducerConfig{
		Endpoint:     "127.0.0.1:8081",
		StartTimeout: 10 * time.Millisecond,
		newProducer: func(cfg *rmqClient.Config, opts ...rmqClient.ProducerOption) (producerAPI, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}

	if err := producer.Start(nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Start(nil) error = %v, want %v", err, ErrContextRequired)
	}
	if err := producer.Start(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start(timeout) error = %v, want deadline exceeded", err)
	}

	message := &Message{Topic: "payment.created", Body: []byte("ok")}
	if _, err := producer.Send(nil, message); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Send(nil) error = %v, want %v", err, ErrContextRequired)
	}

	done := make(chan struct{})
	producer.SendAsync(nil, message, func(ctx context.Context, receipts []*SendReceipt, err error) {
		defer close(done)
		if !errors.Is(err, ErrContextRequired) {
			t.Fatalf("SendAsync(nil) error = %v, want %v", err, ErrContextRequired)
		}
		if ctx == nil {
			t.Fatal("SendAsync(nil) callback ctx = nil")
		}
	})
	<-done

	if _, err := producer.SendFIFO(context.Background(), message, " "); !errors.Is(err, ErrMessageGroupEmpty) {
		t.Fatalf("SendFIFO(blank group) error = %v, want %v", err, ErrMessageGroupEmpty)
	}
}

func gatherMetricNames(t *testing.T, gatherer prometheus.Gatherer) []string {
	t.Helper()

	families, err := gatherer.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}
	return names
}

type fakeProducer struct {
	startDelay      time.Duration
	lastSendMessage *Message
	closed          bool
}

func (f *fakeProducer) Send(ctx context.Context, message *Message) ([]*SendReceipt, error) {
	f.lastSendMessage = cloneMessage(message)
	return []*SendReceipt{{MessageID: "msg-1"}}, nil
}

func (f *fakeProducer) SendAsync(ctx context.Context, message *Message, handler func(context.Context, []*SendReceipt, error)) {
	f.lastSendMessage = cloneMessage(message)
	handler(ctx, []*SendReceipt{{MessageID: "msg-2"}}, nil)
}

func (f *fakeProducer) Start() error {
	if f.startDelay > 0 {
		time.Sleep(f.startDelay)
	}
	return nil
}

func (f *fakeProducer) GracefulStop() error {
	f.closed = true
	return nil
}
