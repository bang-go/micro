package kafka

import (
	"errors"
	"testing"
	"time"
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
	cfg, _, err := prepareConsumerConfig(&ConsumerConfig{
		Brokers:        []string{"localhost:9092"},
		Topic:          "topic",
		Group:          "group",
		PollMaxRecords: 7,
		PollTimeout:    3 * time.Second,
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
}
