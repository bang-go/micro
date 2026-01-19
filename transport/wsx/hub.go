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
	Broadcast(ctx context.Context, msg []byte)

	// SendTo 向特定 UserID 的连接发送消息 (分布式)
	SendTo(ctx context.Context, userID string, msg []byte)

	// Kick 强制断开特定 UserID 的所有连接 (分布式)
	Kick(ctx context.Context, userID string)

	// Join 将特定 UserID 加入房间 (分布式 - 实际上是本地操作，但需要通过 Redis 协调或业务层调用)
	Join(ctx context.Context, userID string, room string)

	// Leave 将特定 UserID 移出房间
	Leave(ctx context.Context, userID string, room string)

	// BroadcastToRoom 向特定房间广播消息 (分布式)
	BroadcastToRoom(ctx context.Context, room string, msg []byte)

	// BroadcastJSON 广播 JSON 消息 (分布式)
	BroadcastJSON(ctx context.Context, v interface{}) error

	// SendJSONTo 向特定用户发送 JSON 消息 (分布式)
	SendJSONTo(ctx context.Context, userID string, v interface{}) error

	// Count 返回当前在线连接数 (本地)
	Count() int64

	// Close 关闭所有连接
	Close()
}

// Internal Protocol for Redis PubSub
type hubMessage struct {
	Type        string            `json:"type"`             // "broadcast", "unicast", "kick", "room_cast", "room_join", "room_leave"
	Target      string            `json:"target,omitempty"` // UserID for unicast/kick, RoomID for room_cast/join/leave
	Payload     []byte            `json:"payload,omitempty"`
	TraceHeader map[string]string `json:"trace_header,omitempty"` // Trace propagation
	UserID      string            `json:"user_id,omitempty"`      // For room_join/room_leave
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

func (h *hubEntity) Kick(ctx context.Context, userID string) {
	hubKick.Inc()

	// Wrap in protocol
	hm := hubMessage{
		Type:   "kick",
		Target: userID,
	}
	h.injectTrace(ctx, &hm)

	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(ctx, h.channel, data)
		return
	}

	// Local fallback
	h.kickLocal(ctx, userID)
}

func (h *hubEntity) Join(ctx context.Context, userID string, room string) {
	hm := hubMessage{
		Type:   "room_join",
		Target: room,
		UserID: userID,
	}
	h.injectTrace(ctx, &hm)
	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(ctx, h.channel, data)
		return
	}

	h.joinLocal(ctx, userID, room)
}

func (h *hubEntity) Leave(ctx context.Context, userID string, room string) {
	hm := hubMessage{
		Type:   "room_leave",
		Target: room,
		UserID: userID,
	}
	h.injectTrace(ctx, &hm)
	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(ctx, h.channel, data)
		return
	}

	h.leaveLocal(ctx, userID, room)
}

func (h *hubEntity) BroadcastToRoom(ctx context.Context, room string, msg []byte) {
	// Wrap in protocol
	hm := hubMessage{
		Type:    "room_cast",
		Target:  room,
		Payload: msg,
	}
	h.injectTrace(ctx, &hm)

	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(ctx, h.channel, data)
		return
	}

	// Local fallback
	h.broadcastToRoomLocal(ctx, room, msg)
}

func (h *hubEntity) Broadcast(ctx context.Context, msg []byte) {
	hubBroadcast.Inc()

	// Wrap in protocol
	hm := hubMessage{
		Type:    "broadcast",
		Payload: msg,
	}
	h.injectTrace(ctx, &hm)

	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(ctx, h.channel, data)
		return
	}

	// Local fallback
	h.broadcastLocal(ctx, msg)
}

func (h *hubEntity) SendTo(ctx context.Context, userID string, msg []byte) {
	// Wrap in protocol
	hm := hubMessage{
		Type:    "unicast",
		Target:  userID,
		Payload: msg,
	}
	h.injectTrace(ctx, &hm)

	data, _ := json.Marshal(hm)

	if h.broker != nil {
		_ = h.broker.Publish(ctx, h.channel, data)
		return
	}

	// Local fallback
	h.sendToLocal(ctx, userID, msg)
}

func (h *hubEntity) BroadcastJSON(ctx context.Context, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.Broadcast(ctx, data)
	return nil
}

func (h *hubEntity) SendJSONTo(ctx context.Context, userID string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.SendTo(ctx, userID, data)
	return nil
}

func (h *hubEntity) handleBrokerMessage(data []byte) {
	var hm hubMessage
	if err := json.Unmarshal(data, &hm); err != nil {
		return
	}

	// Extract Trace
	ctx := h.extractTrace(&hm)

	switch hm.Type {
	case "broadcast":
		h.broadcastLocal(ctx, hm.Payload)
	case "unicast":
		h.sendToLocal(ctx, hm.Target, hm.Payload)
	case "kick":
		h.kickLocal(ctx, hm.Target)
	case "room_cast":
		h.broadcastToRoomLocal(ctx, hm.Target, hm.Payload)
	case "room_join":
		h.joinLocal(ctx, hm.UserID, hm.Target)
	case "room_leave":
		h.leaveLocal(ctx, hm.UserID, hm.Target)
	}
}

// injectTrace adds current span context to hubMessage
func (h *hubEntity) injectTrace(ctx context.Context, hm *hubMessage) {
	if ctx == nil {
		return
	}
	if hm.TraceHeader == nil {
		hm.TraceHeader = make(map[string]string)
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(hm.TraceHeader))
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

func (h *hubEntity) joinLocal(ctx context.Context, userID string, room string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	conns := h.userIndex[userID]
	if len(conns) == 0 {
		return
	}

	for c := range conns {
		currentRooms := c.Rooms()
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
			continue
		}

		if h.rooms[room] == nil {
			h.rooms[room] = make(map[Connect]struct{})
		}
		h.rooms[room][c] = struct{}{}
		c.addRoom(room)
		hubRoomOps.WithLabelValues("join").Inc()
	}
}

func (h *hubEntity) leaveLocal(ctx context.Context, userID string, room string) {
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

func (h *hubEntity) batchSend(ctx context.Context, conns []Connect, msg []byte) {
	// For small number of connections, send sequentially to avoid goroutine overhead
	if len(conns) <= 10 {
		for _, c := range conns {
			sendCtx, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
			_ = c.SendBinary(sendCtx, msg)
			cancel()
		}
		return
	}

	// For larger numbers, use a bit of concurrency
	// We use a semaphore-like approach or just simple goroutines if the number isn't extreme.
	// Considering SendBinary is non-blocking (it just pushes to a channel),
	// the main bottleneck would be many connections with full channels.

	var wg sync.WaitGroup
	// Limit concurrency to avoid spawning too many goroutines at once
	sem := make(chan struct{}, 50)

	for _, c := range conns {
		wg.Add(1)
		go func(conn Connect) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			sendCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
			_ = conn.SendBinary(sendCtx, msg)
			cancel()
		}(c)
	}
	wg.Wait()
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
