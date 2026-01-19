package wsx

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisBroker struct {
	client *redis.Client
	pubsub *redis.PubSub
	mu     sync.Mutex
}

func NewRedisBroker(addr string, password string, db int) *RedisBroker {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisBroker{
		client: rdb,
	}
}

func NewRedisBrokerWithClient(client *redis.Client) *RedisBroker {
	return &RedisBroker{
		client: client,
	}
}

func (b *RedisBroker) Subscribe(ctx context.Context, channel string, handler func(msg []byte)) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Redis PubSub is connection based.
	// For simplicity, we assume one subscription per broker instance for now,
	// or we reuse the pubsub connection.
	if b.pubsub == nil {
		b.pubsub = b.client.Subscribe(ctx, channel)
	} else {
		if err := b.pubsub.Subscribe(ctx, channel); err != nil {
			return err
		}
	}

	// Start a goroutine to listen
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Log panic?
			}
		}()

		for {
			ch := b.pubsub.Channel()
			for msg := range ch {
				if msg.Channel == channel {
					handler([]byte(msg.Payload))
				}
			}

			// If channel is closed, it means pubsub is closed or connection lost.
			// Check if we should exit (if broker is closed)
			b.mu.Lock()
			if b.pubsub == nil {
				b.mu.Unlock()
				return
			}
			// Otherwise, it might be a connection issue, redis-go might handle reconnect,
			// but we should ensure we get a new channel if needed.
			b.mu.Unlock()
			time.Sleep(time.Second)
		}
	}()

	return nil
}

func (b *RedisBroker) Publish(ctx context.Context, channel string, msg []byte) error {
	return b.client.Publish(ctx, channel, msg).Err()
}

func (b *RedisBroker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pubsub != nil {
		_ = b.pubsub.Close()
		b.pubsub = nil
	}
	return b.client.Close()
}
