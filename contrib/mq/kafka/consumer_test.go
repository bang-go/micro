package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestPrepareConsumerConfigRequiresExplicitInputs(t *testing.T) {
	tests := []struct {
		name string
		cfg  *ConsumerConfig
		err  error
	}{
		{name: "nil", cfg: nil, err: ErrNilConsumerConfig},
		{name: "brokers", cfg: &ConsumerConfig{Topic: "t", Group: "g"}, err: ErrBrokersRequired},
		{name: "topic", cfg: &ConsumerConfig{Brokers: []string{"localhost:9092"}, Group: "g"}, err: ErrTopicRequired},
		{name: "group", cfg: &ConsumerConfig{Brokers: []string{"localhost:9092"}, Topic: "t"}, err: ErrConsumerGroupEmpty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := prepareConsumerConfig(tt.cfg)
			if !errors.Is(err, tt.err) {
				t.Fatalf("expected %v, got %v", tt.err, err)
			}
		})
	}
}

func TestPrepareConsumerConfigNormalizesValues(t *testing.T) {
	cfg, _, err := prepareConsumerConfig(&ConsumerConfig{
		Brokers: []string{" localhost:9092 ", ""},
		Topic:   " dts-topic ",
		Group:   " dts-group ",
	})
	if err != nil {
		t.Fatalf("prepare config failed: %v", err)
	}
	if cfg.Name != "dts-group" {
		t.Fatalf("unexpected name: %s", cfg.Name)
	}
	if cfg.ClientID != "dts-group" {
		t.Fatalf("unexpected client id: %s", cfg.ClientID)
	}
	if cfg.Brokers[0] != "localhost:9092" {
		t.Fatalf("unexpected broker: %s", cfg.Brokers[0])
	}
	if cfg.Topic != "dts-topic" {
		t.Fatalf("unexpected topic: %s", cfg.Topic)
	}
	if cfg.Group != "dts-group" {
		t.Fatalf("unexpected group: %s", cfg.Group)
	}
	if cfg.PollMaxRecords != defaultPollMaxRecords {
		t.Fatalf("unexpected max records: %d", cfg.PollMaxRecords)
	}
	if cfg.PollTimeout != defaultPollTimeout {
		t.Fatalf("unexpected poll timeout: %s", cfg.PollTimeout)
	}
}

func TestPrepareConsumerConfigKeepsExplicitPollSettings(t *testing.T) {
	startTimestamp := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	cfg, _, err := prepareConsumerConfig(&ConsumerConfig{
		Brokers:        []string{"localhost:9092"},
		Topic:          "topic",
		Group:          "group",
		PollMaxRecords: 7,
		PollTimeout:    3 * time.Second,
		StartTimestamp: startTimestamp,
	})
	if err != nil {
		t.Fatalf("prepare config failed: %v", err)
	}
	if cfg.PollMaxRecords != 7 {
		t.Fatalf("unexpected max records: %d", cfg.PollMaxRecords)
	}
	if cfg.PollTimeout != 3*time.Second {
		t.Fatalf("unexpected poll timeout: %s", cfg.PollTimeout)
	}
	if !cfg.StartTimestamp.Equal(startTimestamp) {
		t.Fatalf("unexpected start timestamp: %s", cfg.StartTimestamp)
	}
}

func TestPrepareConsumerConfigRejectsIncompleteSASL(t *testing.T) {
	tests := []struct {
		name string
		cfg  *ConsumerConfig
	}{
		{
			name: "username only",
			cfg: &ConsumerConfig{
				Brokers:  []string{"localhost:9092"},
				Topic:    "topic",
				Group:    "group",
				Username: "user",
			},
		},
		{
			name: "password only",
			cfg: &ConsumerConfig{
				Brokers:  []string{"localhost:9092"},
				Topic:    "topic",
				Group:    "group",
				Password: "password",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := prepareConsumerConfig(tt.cfg)
			if !errors.Is(err, ErrSASLConfigInvalid) {
				t.Fatalf("expected %v, got %v", ErrSASLConfigInvalid, err)
			}
		})
	}
}

func TestConsumerStartDoesNotProbeKafka(t *testing.T) {
	fake := &fakeConsumer{fetches: kgo.NewErrFetch(errors.New("should not poll"))}
	consumer := newTestConsumer(t, fake)

	if err := consumer.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if fake.polls != 0 {
		t.Fatalf("poll count = %d, want 0", fake.polls)
	}
}

func TestConsumerStartRequiresContext(t *testing.T) {
	consumer := newTestConsumer(t, &fakeConsumer{})

	if err := consumer.Start(nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Start(nil) error = %v, want %v", err, ErrContextRequired)
	}
}

func TestConsumerPollTreatsOwnDeadlineAsEmpty(t *testing.T) {
	fake := &fakeConsumer{fetches: kgo.NewErrFetch(context.DeadlineExceeded)}
	consumer := newTestConsumer(t, fake)

	messages, err := consumer.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("message count = %d, want 0", len(messages))
	}
}

func TestConsumerPollKeepsParentContextError(t *testing.T) {
	fake := &fakeConsumer{fetches: kgo.NewErrFetch(context.DeadlineExceeded)}
	consumer := newTestConsumer(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := consumer.Poll(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Poll() error = %v, want context canceled", err)
	}
}

func TestConsumerCloseAllowsRebalance(t *testing.T) {
	fake := &fakeConsumer{}
	consumer := newTestConsumer(t, fake)

	if err := consumer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !fake.closedAllowingRebalance {
		t.Fatal("Close() did not call CloseAllowingRebalance")
	}
	if fake.closed {
		t.Fatal("Close() called Close instead of CloseAllowingRebalance")
	}
}

func newTestConsumer(t *testing.T, fake *fakeConsumer) Consumer {
	t.Helper()
	consumer, err := NewConsumer(&ConsumerConfig{
		Brokers:        []string{"localhost:9092"},
		Topic:          "topic",
		Group:          "group",
		PollTimeout:    time.Millisecond,
		DisableMetrics: true,
		newConsumer: func(...kgo.Opt) (consumerAPI, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	return consumer
}

type fakeConsumer struct {
	fetches                 kgo.Fetches
	commitErr               error
	polls                   int
	allowRebalances         int
	closed                  bool
	closedAllowingRebalance bool
}

func (f *fakeConsumer) PollRecords(context.Context, int) kgo.Fetches {
	f.polls++
	return f.fetches
}

func (f *fakeConsumer) CommitRecords(context.Context, ...*kgo.Record) error {
	return f.commitErr
}

func (f *fakeConsumer) AllowRebalance() {
	f.allowRebalances++
}

func (f *fakeConsumer) Close() {
	f.closed = true
}

func (f *fakeConsumer) CloseAllowingRebalance() {
	f.closedAllowingRebalance = true
}
