package wsx

import "context"

type Subscription interface {
	Close(context.Context) error
}

type MessageBroker interface {
	Subscribe(context.Context, string, func([]byte)) (Subscription, error)
	Publish(context.Context, string, []byte) (int64, error)
	Shutdown(context.Context) error
}
