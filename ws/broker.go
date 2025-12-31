package ws

import (
	"context"
)

// MessageBroker 定义消息代理接口，用于跨节点通信
type MessageBroker interface {
	// Subscribe 订阅频道，当收到消息时调用 handler
	Subscribe(ctx context.Context, channel string, handler func(msg []byte)) error
	// Publish 发布消息到频道
	Publish(ctx context.Context, channel string, msg []byte) error
	// Close 关闭代理连接
	Close() error
}
