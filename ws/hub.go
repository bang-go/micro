package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/bang-go/opt"
)

// Hub 管理所有活跃连接，支持广播和单播
type Hub interface {
	// Register 注册连接
	Register(Connect)
	// Unregister 注销连接
	Unregister(Connect)

	// Broadcast 广播消息给所有连接 (分布式)
	Broadcast(msg []byte)

	// SendTo 向特定 UserID 的连接发送消息 (分布式)
	SendTo(userID string, msg []byte)

	// Count 返回当前在线连接数 (本地)
	Count() int64

	// Close 关闭所有连接
	Close()
}

// Internal Protocol for Redis PubSub
type hubMessage struct {
	Type    string `json:"type"`             // "broadcast", "unicast"
	Target  string `json:"target,omitempty"` // UserID for unicast
	Payload []byte `json:"payload"`
}

type hubEntity struct {
	mu          sync.RWMutex
	connections map[Connect]struct{}
	// userIndex maps UserID -> []Connect (one user might have multiple devices)
	userIndex map[string]map[Connect]struct{}

	broker  MessageBroker
	channel string
}

func NewHub(opts ...opt.Option[hubOptions]) Hub {
	options := &hubOptions{
		channel: "ws:global",
	}
	opt.Each(options, opts...)

	h := &hubEntity{
		connections: make(map[Connect]struct{}),
		userIndex:   make(map[string]map[Connect]struct{}),
		broker:      options.broker,
		channel:     options.channel,
	}

	if h.broker != nil {
		_ = h.broker.Subscribe(context.Background(), h.channel, h.handleBrokerMessage)
	}

	return h
}

// hubOptions and Option helpers
type hubOptions struct {
	broker  MessageBroker
	channel string
}

func WithHubBroker(broker MessageBroker) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) {
		o.broker = broker
	})
}

func WithHubChannel(channel string) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) {
		o.channel = channel
	})
}

func (h *hubEntity) Register(c Connect) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connections[c] = struct{}{}

	// Index by UserID if present
	uid := c.ID()
	if uid != "" {
		if h.userIndex[uid] == nil {
			h.userIndex[uid] = make(map[Connect]struct{})
		}
		h.userIndex[uid][c] = struct{}{}
	}
}

func (h *hubEntity) Unregister(c Connect) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.connections[c]; ok {
		delete(h.connections, c)

		// Remove from index
		uid := c.ID()
		if uid != "" && h.userIndex[uid] != nil {
			delete(h.userIndex[uid], c)
			if len(h.userIndex[uid]) == 0 {
				delete(h.userIndex, uid)
			}
		}
	}
}

func (h *hubEntity) Broadcast(msg []byte) {
	hubBroadcast.Inc()

	// Wrap in protocol
	hm := hubMessage{
		Type:    "broadcast",
		Payload: msg,
	}

	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(context.Background(), h.channel, data)
		return
	}

	// Local fallback
	h.broadcastLocal(msg)
}

func (h *hubEntity) SendTo(userID string, msg []byte) {
	// Wrap in protocol
	hm := hubMessage{
		Type:    "unicast",
		Target:  userID,
		Payload: msg,
	}

	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(context.Background(), h.channel, data)
		return
	}

	// Local fallback
	h.sendToLocal(userID, msg)
}

func (h *hubEntity) handleBrokerMessage(data []byte) {
	var hm hubMessage
	if err := json.Unmarshal(data, &hm); err != nil {
		return
	}

	switch hm.Type {
	case "broadcast":
		h.broadcastLocal(hm.Payload)
	case "unicast":
		h.sendToLocal(hm.Target, hm.Payload)
	}
}

func (h *hubEntity) broadcastLocal(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for c := range h.connections {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		_ = c.SendBinary(ctx, msg)
		cancel()
	}
}

func (h *hubEntity) sendToLocal(userID string, msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	conns := h.userIndex[userID]
	for c := range conns {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		_ = c.SendBinary(ctx, msg)
		cancel()
	}
}

func (h *hubEntity) Count() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return int64(len(h.connections))
}

func (h *hubEntity) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for c := range h.connections {
		_ = c.Close()
	}
	h.connections = make(map[Connect]struct{})
	h.userIndex = make(map[string]map[Connect]struct{})
}
