package wsx

import "testing"

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

func TestRedisBrokerInvokeHandlerSafely(t *testing.T) {
	t.Parallel()

	invokeHandlerSafely(func([]byte) {
		panic("boom")
	}, []byte("msg"))
}

func TestRedisSubscriberQueueIsBounded(t *testing.T) {
	t.Parallel()

	subscriber := newRedisSubscriber(func([]byte) {})
	defer subscriber.close()

	if got := cap(subscriber.queue); got != redisSubscriberQueueSize {
		t.Fatalf("unexpected subscriber queue capacity: %d", got)
	}
}
