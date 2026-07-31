package wsx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisSubscriberQueueSize = 1024

type RedisBroker struct {
	client     *redis.Client
	ownsClient bool

	mu            sync.RWMutex
	pubsub        *redis.PubSub
	handlers      map[string]map[uint64]*redisSubscriber
	nextHandlerID uint64
	closed        bool

	readerWG sync.WaitGroup
	workerWG sync.WaitGroup
	closeMu  sync.Mutex
	closeErr error
}

func NewRedisBroker(addr string, password string, db int) (*RedisBroker, error) {
	if addr == "" {
		return nil, ErrBrokerClientRequired
	}
	client := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	return newRedisBroker(client, true)
}

func NewRedisBrokerWithClient(client *redis.Client) (*RedisBroker, error) {
	return newRedisBroker(client, false)
}

func newRedisBroker(client *redis.Client, ownsClient bool) (*RedisBroker, error) {
	if client == nil {
		return nil, ErrBrokerClientRequired
	}
	return &RedisBroker{
		client:     client,
		ownsClient: ownsClient,
		handlers:   make(map[string]map[uint64]*redisSubscriber),
	}, nil
}

func (b *RedisBroker) Subscribe(ctx context.Context, channel string, handler func([]byte)) (Subscription, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if handler == nil {
		return nil, ErrBrokerHandlerRequired
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, ErrBrokerClosed
	}
	pubsub := b.pubsub
	startReader := false
	if pubsub == nil {
		pubsub = b.client.Subscribe(ctx, channel)
		if _, err := pubsub.Receive(ctx); err != nil {
			b.mu.Unlock()
			return nil, errors.Join(err, pubsub.Close())
		}
		b.pubsub = pubsub
		startReader = true
	} else if len(b.handlers[channel]) == 0 {
		if err := pubsub.Subscribe(ctx, channel); err != nil {
			b.mu.Unlock()
			return nil, err
		}
	}

	id := b.nextHandlerID
	b.nextHandlerID++
	subscriber := newRedisSubscriber(handler, &b.workerWG)
	if b.handlers[channel] == nil {
		b.handlers[channel] = make(map[uint64]*redisSubscriber)
	}
	b.handlers[channel][id] = subscriber
	if startReader {
		b.readerWG.Add(1)
	}
	b.mu.Unlock()

	if startReader {
		go b.readLoop(pubsub)
	}

	return &redisSubscription{broker: b, channel: channel, id: id}, nil
}

func (b *RedisBroker) Publish(ctx context.Context, channel string, msg []byte) (int64, error) {
	if err := validateContext(ctx); err != nil {
		return 0, err
	}
	b.mu.RLock()
	closed := b.closed
	client := b.client
	b.mu.RUnlock()
	if closed {
		return 0, ErrBrokerClosed
	}
	count, err := client.Publish(ctx, channel, msg).Result()
	if err != nil {
		redisBrokerErrors.WithLabelValues("publish").Inc()
	}
	return count, err
}

func (b *RedisBroker) Shutdown(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}

	b.closeMu.Lock()
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		pubsub := b.pubsub
		b.pubsub = nil
		subscribers := b.snapshotSubscribersLocked()
		b.handlers = make(map[string]map[uint64]*redisSubscriber)
		client := b.client
		ownsClient := b.ownsClient
		b.mu.Unlock()

		for _, subscriber := range subscribers {
			subscriber.close()
		}
		if pubsub != nil {
			b.closeErr = errors.Join(b.closeErr, pubsub.Close())
		}
		if ownsClient {
			b.closeErr = errors.Join(b.closeErr, client.Close())
		}
	} else {
		b.mu.Unlock()
	}
	closeErr := b.closeErr
	b.closeMu.Unlock()

	done := make(chan struct{})
	go func() {
		b.readerWG.Wait()
		b.workerWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return closeErr
	case <-ctx.Done():
		return errors.Join(closeErr, ctx.Err())
	}
}

func (b *RedisBroker) closeSubscription(ctx context.Context, channel string, id uint64) error {
	if err := validateContext(ctx); err != nil {
		return err
	}

	b.mu.Lock()
	handlers := b.handlers[channel]
	subscriber := handlers[id]
	if subscriber == nil {
		b.mu.Unlock()
		return nil
	}
	if len(handlers) == 1 && !b.closed && b.pubsub != nil {
		if err := b.pubsub.Unsubscribe(ctx, channel); err != nil {
			b.mu.Unlock()
			return err
		}
	}
	delete(handlers, id)
	if len(handlers) == 0 {
		delete(b.handlers, channel)
	}
	b.mu.Unlock()
	subscriber.close()
	return nil
}

func (b *RedisBroker) readLoop(pubsub *redis.PubSub) {
	defer b.readerWG.Done()
	for {
		msg, err := pubsub.ReceiveMessage(context.Background())
		if err != nil {
			b.mu.RLock()
			stop := b.closed || b.pubsub != pubsub
			b.mu.RUnlock()
			if stop {
				return
			}
			redisBrokerErrors.WithLabelValues("receive").Inc()
			time.Sleep(100 * time.Millisecond)
			continue
		}
		for _, subscriber := range b.snapshotSubscribers(msg.Channel) {
			subscriber.dispatch([]byte(msg.Payload))
		}
	}
}

func (b *RedisBroker) snapshotSubscribers(channel string) []*redisSubscriber {
	b.mu.RLock()
	defer b.mu.RUnlock()
	registered := b.handlers[channel]
	result := make([]*redisSubscriber, 0, len(registered))
	for _, subscriber := range registered {
		result = append(result, subscriber)
	}
	return result
}

func (b *RedisBroker) snapshotSubscribersLocked() []*redisSubscriber {
	result := make([]*redisSubscriber, 0)
	for _, handlers := range b.handlers {
		for _, subscriber := range handlers {
			result = append(result, subscriber)
		}
	}
	return result
}

type redisSubscription struct {
	broker  *RedisBroker
	channel string
	id      uint64
	closed  atomic.Bool
}

func (s *redisSubscription) Close(ctx context.Context) error {
	if s.closed.Load() {
		return nil
	}
	if err := s.broker.closeSubscription(ctx, s.channel, s.id); err != nil {
		return err
	}
	s.closed.Store(true)
	return nil
}

type redisSubscriber struct {
	handler   func([]byte)
	queue     chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	isClosed  atomic.Bool
	wg        *sync.WaitGroup
}

func newRedisSubscriber(handler func([]byte), wg *sync.WaitGroup) *redisSubscriber {
	s := &redisSubscriber{
		handler: handler,
		queue:   make(chan []byte, redisSubscriberQueueSize),
		closed:  make(chan struct{}),
		wg:      wg,
	}
	wg.Add(1)
	go s.run()
	return s
}

func (s *redisSubscriber) dispatch(msg []byte) {
	if s.isClosed.Load() {
		return
	}
	cloned := append([]byte(nil), msg...)
	select {
	case s.queue <- cloned:
	case <-s.closed:
	}
}

func (s *redisSubscriber) close() {
	s.closeOnce.Do(func() {
		s.isClosed.Store(true)
		close(s.closed)
	})
}

func (s *redisSubscriber) run() {
	defer s.wg.Done()
	for {
		select {
		case msg := <-s.queue:
			if !s.isClosed.Load() {
				s.handler(msg)
			}
		case <-s.closed:
			return
		}
	}
}
