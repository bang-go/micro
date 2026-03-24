package wsx

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisBroker struct {
	client        *redis.Client
	ownsClient    bool
	pubsub        *redis.PubSub
	mu            sync.RWMutex
	handlers      map[string]map[uint64]*redisSubscriber
	nextHandlerID uint64
	closed        bool
	closeOnce     sync.Once
}

func NewRedisBroker(addr string, password string, db int) *RedisBroker {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisBroker{
		client:     rdb,
		ownsClient: true,
		handlers:   make(map[string]map[uint64]*redisSubscriber),
	}
}

func NewRedisBrokerWithClient(client *redis.Client) *RedisBroker {
	return &RedisBroker{
		client:   client,
		handlers: make(map[string]map[uint64]*redisSubscriber),
	}
}

func (b *RedisBroker) Subscribe(ctx context.Context, channel string, handler func(msg []byte)) error {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if handler == nil {
		return errBrokerHandlerMissing
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errBrokerClosed
	}

	subscriber := newRedisSubscriber(handler)
	firstHandlerForChannel := len(b.handlers[channel]) == 0
	if b.handlers[channel] == nil {
		b.handlers[channel] = make(map[uint64]*redisSubscriber)
	}
	handlerID := b.nextHandlerID
	b.nextHandlerID++
	b.handlers[channel][handlerID] = subscriber

	pubsub := b.pubsub
	startReader := false
	if pubsub == nil {
		pubsub = b.client.Subscribe(ctx, channel)
		b.pubsub = pubsub
		startReader = true
	}
	b.mu.Unlock()

	if startReader {
		if err := b.awaitSubscription(ctx, pubsub); err != nil {
			b.mu.Lock()
			if b.pubsub == pubsub {
				b.pubsub = nil
			}
			b.mu.Unlock()
			subscriber.close()
			b.removeHandler(channel, handlerID, false)
			_ = pubsub.Close()
			return err
		}
		go b.readLoop(pubsub)
	}

	if !startReader && firstHandlerForChannel {
		if err := pubsub.Subscribe(ctx, channel); err != nil {
			subscriber.close()
			b.removeHandler(channel, handlerID, false)
			return err
		}
	}

	go b.unsubscribeOnDone(ctx, channel, handlerID)

	return nil
}

func (b *RedisBroker) Publish(ctx context.Context, channel string, msg []byte) error {
	ctx = normalizeContext(ctx)

	b.mu.RLock()
	closed := b.closed
	client := b.client
	b.mu.RUnlock()
	if closed {
		return errBrokerClosed
	}
	return client.Publish(ctx, channel, msg).Err()
}

func (b *RedisBroker) NumSubscribers(ctx context.Context, channel string) (int64, error) {
	ctx = normalizeContext(ctx)

	b.mu.RLock()
	closed := b.closed
	client := b.client
	b.mu.RUnlock()
	if closed {
		return 0, errBrokerClosed
	}

	result, err := client.PubSubNumSub(ctx, channel).Result()
	if err != nil {
		return 0, err
	}
	return result[channel], nil
}

func (b *RedisBroker) Close() error {
	var closeErr error

	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		pubsub := b.pubsub
		client := b.client
		subscribers := b.snapshotSubscribersLocked()
		b.pubsub = nil
		b.handlers = make(map[string]map[uint64]*redisSubscriber)
		b.mu.Unlock()

		for _, subscriber := range subscribers {
			subscriber.close()
		}
		if pubsub != nil {
			closeErr = errors.Join(closeErr, pubsub.Close())
		}
		if b.ownsClient && client != nil {
			closeErr = errors.Join(closeErr, client.Close())
		}
	})

	return closeErr
}

func (b *RedisBroker) readLoop(pubsub *redis.PubSub) {
	for {
		msg, err := pubsub.ReceiveMessage(context.Background())
		if err != nil {
			if b.shouldStop(pubsub) {
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		for _, subscriber := range b.snapshotSubscribers(msg.Channel) {
			subscriber.dispatch([]byte(msg.Payload))
		}
	}
}

func (b *RedisBroker) unsubscribeOnDone(ctx context.Context, channel string, handlerID uint64) {
	<-ctx.Done()
	b.removeHandler(channel, handlerID, true)
}

func (b *RedisBroker) removeHandler(channel string, handlerID uint64, unsubscribe bool) {
	b.mu.Lock()
	handlers := b.handlers[channel]
	if len(handlers) == 0 {
		b.mu.Unlock()
		return
	}
	if _, ok := handlers[handlerID]; !ok {
		b.mu.Unlock()
		return
	}

	subscriber := handlers[handlerID]
	delete(handlers, handlerID)
	if len(handlers) > 0 {
		b.mu.Unlock()
		subscriber.close()
		return
	}

	delete(b.handlers, channel)
	pubsub := b.pubsub
	closed := b.closed
	b.mu.Unlock()

	subscriber.close()
	if unsubscribe && !closed && pubsub != nil {
		_ = pubsub.Unsubscribe(context.Background(), channel)
	}
}

func (b *RedisBroker) snapshotSubscribers(channel string) []*redisSubscriber {
	b.mu.RLock()
	defer b.mu.RUnlock()

	registered := b.handlers[channel]
	if len(registered) == 0 {
		return nil
	}

	handlers := make([]*redisSubscriber, 0, len(registered))
	for _, handler := range registered {
		handlers = append(handlers, handler)
	}
	return handlers
}

func (b *RedisBroker) snapshotSubscribersLocked() []*redisSubscriber {
	subscribers := make([]*redisSubscriber, 0)
	for _, handlers := range b.handlers {
		for _, subscriber := range handlers {
			subscribers = append(subscribers, subscriber)
		}
	}
	return subscribers
}

func (b *RedisBroker) shouldStop(pubsub *redis.PubSub) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.closed || b.pubsub != pubsub
}

func (b *RedisBroker) awaitSubscription(ctx context.Context, pubsub *redis.PubSub) error {
	_, err := pubsub.Receive(ctx)
	return err
}

func invokeHandlerSafely(handler func([]byte), msg []byte) {
	defer func() {
		_ = recover()
	}()
	handler(msg)
}

const redisSubscriberQueueSize = 1024

type redisSubscriber struct {
	handler  func([]byte)
	queue    chan []byte
	closed   chan struct{}
	closeMu  sync.Mutex
	isClosed bool
}

func newRedisSubscriber(handler func([]byte)) *redisSubscriber {
	subscriber := &redisSubscriber{
		handler: handler,
		queue:   make(chan []byte, redisSubscriberQueueSize),
		closed:  make(chan struct{}),
	}
	go subscriber.run()
	return subscriber
}

func (s *redisSubscriber) dispatch(msg []byte) {
	cloned := append([]byte(nil), msg...)
	defer func() {
		_ = recover()
	}()

	select {
	case s.queue <- cloned:
	case <-s.closed:
	}
}

func (s *redisSubscriber) close() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.isClosed {
		return
	}
	s.isClosed = true
	close(s.closed)
	close(s.queue)
}

func (s *redisSubscriber) run() {
	for msg := range s.queue {
		invokeHandlerSafely(s.handler, msg)
	}
}
