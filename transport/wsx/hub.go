package wsx

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/bang-go/opt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
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

	// Kick 强制断开特定 UserID 的所有连接 (分布式)
	Kick(userID string)

	// Join 将特定 UserID 加入房间 (分布式 - 实际上是本地操作，但需要通过 Redis 协调或业务层调用)
	// 这里的 Join 是指将 UserID 的当前和未来连接关联到 room。
	// 但通常 WebSocket 的 Room 是临时的，绑定在 Connection 上。
	// 考虑到 userIndex，我们可以让 Join 作用于 userID 当前的所有连接。
	Join(userID string, room string)

	// Leave 将特定 UserID 移出房间
	Leave(userID string, room string)

	// BroadcastToRoom 向特定房间广播消息 (分布式)
	BroadcastToRoom(room string, msg []byte)

	// Count 返回当前在线连接数 (本地)
	Count() int64

	// Close 关闭所有连接
	Close()
}

// Internal Protocol for Redis PubSub
type hubMessage struct {
	Type        string            `json:"type"`             // "broadcast", "unicast", "kick", "room_cast"
	Target      string            `json:"target,omitempty"` // UserID for unicast/kick, RoomID for room_cast
	Payload     []byte            `json:"payload,omitempty"`
	TraceHeader map[string]string `json:"trace_header,omitempty"` // Trace propagation
}

type hubEntity struct {
	mu          sync.RWMutex
	connections map[Connect]struct{}
	// userIndex maps UserID -> []Connect (one user might have multiple devices)
	userIndex map[string]map[Connect]struct{}
	// rooms maps RoomID -> []Connect
	rooms map[string]map[Connect]struct{}

	broker             MessageBroker
	channel            string
	maxRoomsPerConnect int
}

func NewHub(opts ...opt.Option[hubOptions]) Hub {
	options := &hubOptions{
		channel:            "ws:global",
		maxRoomsPerConnect: 50,
	}
	opt.Each(options, opts...)

	h := &hubEntity{
		connections:        make(map[Connect]struct{}),
		userIndex:          make(map[string]map[Connect]struct{}),
		rooms:              make(map[string]map[Connect]struct{}),
		broker:             options.broker,
		channel:            options.channel,
		maxRoomsPerConnect: options.maxRoomsPerConnect,
	}

	if h.broker != nil {
		_ = h.broker.Subscribe(context.Background(), h.channel, h.handleBrokerMessage)
	}

	return h
}

// hubOptions and Option helpers
type hubOptions struct {
	broker             MessageBroker
	channel            string
	maxRoomsPerConnect int
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

func WithHubMaxRoomsPerConnect(max int) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) {
		o.maxRoomsPerConnect = max
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

		// Optimized removal from rooms
		rooms := c.Rooms()
		for _, room := range rooms {
			if conns, ok := h.rooms[room]; ok {
				delete(conns, c)
				if len(conns) == 0 {
					delete(h.rooms, room)
				}
			}
			// c.removeRoom(room) // Not strictly needed as c is closing, but good for consistency
		}
	}
}

func (h *hubEntity) Kick(userID string) {
	hubKick.Inc()

	// Wrap in protocol
	hm := hubMessage{
		Type:   "kick",
		Target: userID,
	}
	h.injectTrace(&hm)

	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(context.Background(), h.channel, data)
		return
	}

	// Local fallback
	h.kickLocal(context.Background(), userID)
}

func (h *hubEntity) Join(userID string, room string) {
	// Join is local operation but we need to ensure all connections of this user join the room.
	// Since userIndex is local, we only operate locally.
	// Wait, if Join is called on Pod A, but user is on Pod B?
	// Redis PubSub doesn't support "Join Room" command usually unless we broadcast "Join" instruction.
	// But usually Join is initiated by the user connection (e.g. HTTP request to Pod A where user is connected).
	// IF the user is connected to Pod B, and Pod A receives "Join", we have a problem.
	// However, typically "Join" happens on the node where the connection exists.
	// So we assume Join is local.
	h.mu.Lock()
	defer h.mu.Unlock()

	conns := h.userIndex[userID]
	if len(conns) == 0 {
		return
	}

	// Check limit for each connection
	// If one user has multiple devices, we check limit for each device independently.
	// But Join adds all devices to the room.
	// We need to iterate and check limit.
	for c := range conns {
		currentRooms := c.Rooms()
		// Check if already in room
		inRoom := false
		for _, r := range currentRooms {
			if r == room {
				inRoom = true
				break
			}
		}
		if inRoom {
			continue
		}

		if len(currentRooms) >= h.maxRoomsPerConnect {
			limitExceeded.WithLabelValues("max_rooms").Inc()
			continue // Skip this connection, or error?
			// Since Join is "best effort" for all devices, skipping full devices is reasonable.
		}

		if h.rooms[room] == nil {
			h.rooms[room] = make(map[Connect]struct{})
		}
		h.rooms[room][c] = struct{}{}
		c.addRoom(room)
		hubRoomOps.WithLabelValues("join").Inc()
	}
}

func (h *hubEntity) Leave(userID string, room string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	conns := h.userIndex[userID]
	if len(conns) == 0 {
		return
	}

	if h.rooms[room] == nil {
		return
	}

	for c := range conns {
		if _, ok := h.rooms[room][c]; ok {
			delete(h.rooms[room], c)
			c.removeRoom(room)
			hubRoomOps.WithLabelValues("leave").Inc()
		}
	}

	if len(h.rooms[room]) == 0 {
		delete(h.rooms, room)
	}
}

func (h *hubEntity) BroadcastToRoom(room string, msg []byte) {
	// Wrap in protocol
	hm := hubMessage{
		Type:    "room_cast",
		Target:  room,
		Payload: msg,
	}
	h.injectTrace(&hm)

	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(context.Background(), h.channel, data)
		return
	}

	// Local fallback
	h.broadcastToRoomLocal(context.Background(), room, msg)
}

func (h *hubEntity) Broadcast(msg []byte) {
	hubBroadcast.Inc()

	// Wrap in protocol
	hm := hubMessage{
		Type:    "broadcast",
		Payload: msg,
	}
	h.injectTrace(&hm)

	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(context.Background(), h.channel, data)
		return
	}

	// Local fallback
	h.broadcastLocal(context.Background(), msg)
}

func (h *hubEntity) SendTo(userID string, msg []byte) {
	// Wrap in protocol
	hm := hubMessage{
		Type:    "unicast",
		Target:  userID,
		Payload: msg,
	}
	h.injectTrace(&hm)

	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(context.Background(), h.channel, data)
		return
	}

	// Local fallback
	h.sendToLocal(context.Background(), userID, msg)
}

func (h *hubEntity) handleBrokerMessage(data []byte) {
	var hm hubMessage
	if err := json.Unmarshal(data, &hm); err != nil {
		return
	}

	// Extract Trace
	ctx := h.extractTrace(&hm)
	// Currently we don't pass ctx to local methods (broadcastLocal etc take context.Background with timeout)
	// But we can use it for logging or creating a child span here if we want to trace the processing latency.
	// For now, let's at least keep the context available if we expand local methods to accept it.
	_ = ctx

	switch hm.Type {
	case "broadcast":
		h.broadcastLocal(ctx, hm.Payload)
	case "unicast":
		h.sendToLocal(ctx, hm.Target, hm.Payload)
	case "kick":
		h.kickLocal(ctx, hm.Target)
	case "room_cast":
		h.broadcastToRoomLocal(ctx, hm.Target, hm.Payload)
	}
}

// injectTrace adds current span context to hubMessage
func (h *hubEntity) injectTrace(hm *hubMessage) {
	// For now we use background context as Hub interface doesn't support context yet.
	// But if we had one, we would inject it here.
	// We can use otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(hm.TraceHeader))
	// Since we don't have ctx, we skip injection for now or inject empty.
	// To fully support trace, we need to change Hub interface to accept Context.
}

// extractTrace gets context from hubMessage
func (h *hubEntity) extractTrace(hm *hubMessage) context.Context {
	if hm.TraceHeader == nil {
		return context.Background()
	}
	carrier := propagation.MapCarrier(hm.TraceHeader)
	return otel.GetTextMapPropagator().Extract(context.Background(), carrier)
}

func (h *hubEntity) broadcastLocal(ctx context.Context, msg []byte) {
	h.mu.RLock()
	// Snapshot connections to avoid holding lock during send
	conns := make([]Connect, 0, len(h.connections))
	for c := range h.connections {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	h.batchSend(ctx, conns, msg)
}

func (h *hubEntity) sendToLocal(ctx context.Context, userID string, msg []byte) {
	h.mu.RLock()
	// Snapshot connections
	targetConns := h.userIndex[userID]
	conns := make([]Connect, 0, len(targetConns))
	for c := range targetConns {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	h.batchSend(ctx, conns, msg)
}

func (h *hubEntity) kickLocal(ctx context.Context, userID string) {
	h.mu.RLock()
	targetConns := h.userIndex[userID]
	conns := make([]Connect, 0, len(targetConns))
	for c := range targetConns {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	for _, c := range conns {
		_ = c.Close()
	}
}

func (h *hubEntity) broadcastToRoomLocal(ctx context.Context, room string, msg []byte) {
	h.mu.RLock()
	targetConns := h.rooms[room]
	conns := make([]Connect, 0, len(targetConns))
	for c := range targetConns {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	h.batchSend(ctx, conns, msg)
}

func (h *hubEntity) batchSend(ctx context.Context, conns []Connect, msg []byte) {
	for _, c := range conns {
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
		_ = c.SendBinary(sendCtx, msg)
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
	// Snapshot to avoid holding lock during Close which might trigger callbacks or be slow
	conns := make([]Connect, 0, len(h.connections))
	for c := range h.connections {
		conns = append(conns, c)
	}
	// Clear maps immediately to prevent further operations
	h.connections = make(map[Connect]struct{})
	h.userIndex = make(map[string]map[Connect]struct{})
	h.rooms = make(map[string]map[Connect]struct{})
	h.mu.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}

	if h.broker != nil {
		_ = h.broker.Close()
	}
}
