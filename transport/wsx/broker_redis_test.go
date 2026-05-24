package wsx

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRedisBrokerRemoveHandlerKeepsChannelUntilLastSubscriber(t *testing.T) {
	t.Parallel()

	broker := &RedisBroker{
		handlers: map[string]map[uint64]*redisSubscriber{
			"room": {
				1: newRedisSubscriber(func([]byte) {}),
				2: newRedisSubscriber(func([]byte) {}),
			},
		},
	}

	broker.removeHandler("room", 1, false)
	if got := len(broker.handlers["room"]); got != 1 {
		t.Fatalf("unexpected remaining handler count: %d", got)
	}

	broker.removeHandler("room", 2, false)
	if _, ok := broker.handlers["room"]; ok {
		t.Fatal("expected channel handlers to be removed after last subscriber")
	}
}

func TestRedisBrokerRequiresClient(t *testing.T) {
	t.Parallel()

	broker := NewRedisBrokerWithClient(nil)
	if err := broker.Publish(context.Background(), "channel", []byte("msg")); !errors.Is(err, errBrokerClientMissing) {
		t.Fatalf("expected missing client error on publish, got %v", err)
	}
	if _, err := broker.NumSubscribers(context.Background(), "channel"); !errors.Is(err, errBrokerClientMissing) {
		t.Fatalf("expected missing client error on num subscribers, got %v", err)
	}
	if err := broker.Subscribe(context.Background(), "channel", func([]byte) {}); !errors.Is(err, errBrokerClientMissing) {
		t.Fatalf("expected missing client error on subscribe, got %v", err)
	}
}

func TestRedisSubscriberQueueIsBounded(t *testing.T) {
	t.Parallel()

	subscriber := newRedisSubscriber(func([]byte) {})
	defer subscriber.close()

	if got := cap(subscriber.queue); got != redisSubscriberQueueSize {
		t.Fatalf("unexpected subscriber queue capacity: %d", got)
	}
}

func TestRedisSubscriberDoesNotDispatchAfterClose(t *testing.T) {
	t.Parallel()

	called := make(chan struct{}, 1)
	subscriber := newRedisSubscriber(func([]byte) {
		called <- struct{}{}
	})
	subscriber.close()
	subscriber.dispatch([]byte("late"))

	select {
	case <-called:
		t.Fatal("subscriber dispatched message after close")
	case <-time.After(50 * time.Millisecond):
	}
}
