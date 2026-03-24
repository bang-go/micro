package wsx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bang-go/opt"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Hub 管理所有活跃连接，支持广播和单播
type Hub interface {
	// Register 注册连接
	Register(Connect) error
	// Unregister 注销连接
	Unregister(Connect)

	// Broadcast 广播消息给所有连接 (分布式)
	Broadcast(ctx context.Context, msg []byte) error

	// SendTo 向特定 UserID 的连接发送消息 (分布式)
	SendTo(ctx context.Context, userID string, msg []byte) error

	// SendToWithOrigin 向特定 UserID 的连接发送消息，但排除指定的 OriginID (分布式)
	SendToWithOrigin(ctx context.Context, userID string, msg []byte, originID string) error

	// Kick 强制断开特定 UserID 的所有连接 (分布式)
	Kick(ctx context.Context, userID string) error

	// KickWithOrigin 强制断开特定 UserID 的所有连接，但排除指定的 OriginID (分布式)
	KickWithOrigin(ctx context.Context, userID string, originID string) error

	// Join 将特定 SessionID 加入房间。
	// 房间成员关系是 Session 级别；配置 broker 后仅房间广播会分布式传播。
	Join(ctx context.Context, sessionID string, room string) error

	// Leave 将特定 SessionID 移出房间。
	Leave(ctx context.Context, sessionID string, room string) error

	// BroadcastToRoom 向特定房间广播消息。
	// 配置 broker 后消息会只发送到订阅该房间的节点。
	BroadcastToRoom(ctx context.Context, room string, msg []byte) error

	// BroadcastJSON 广播 JSON 消息 (分布式)
	BroadcastJSON(ctx context.Context, v interface{}) error

	// SendJSONTo 向特定用户发送 JSON 消息 (分布式)
	SendJSONTo(ctx context.Context, userID string, v interface{}) error

	// SendJSONToWithOrigin 向特定用户发送 JSON 消息，但排除指定的 OriginID (分布式)
	SendJSONToWithOrigin(ctx context.Context, userID string, v interface{}, originID string) error

	// Count 返回当前在线连接数 (本地)
	Count() int64

	// Close 关闭所有连接
	Close()
}

// Internal Protocol for Redis PubSub
type hubMessage struct {
	Type        string            `json:"type"` // "broadcast", "unicast", "kick"
	RequestID   string            `json:"request_id,omitempty"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	NodeID      string            `json:"node_id,omitempty"`
	Target      string            `json:"target,omitempty"` // UserID for unicast/kick
	Payload     []byte            `json:"payload,omitempty"`
	TraceHeader map[string]string `json:"trace_header,omitempty"` // Trace propagation
	OriginID    string            `json:"origin_id,omitempty"`    // To avoid self-kicking
	Error       string            `json:"error,omitempty"`
}

type roomMessage struct {
	Payload     []byte            `json:"payload,omitempty"`
	TraceHeader map[string]string `json:"trace_header,omitempty"`
}

type hubEntity struct {
	mu          sync.RWMutex
	connections map[Connect]struct{}
	// userIndex maps UserID -> []Connect (one user might have multiple devices)
	userIndex map[string]map[Connect]struct{}
	// sessionIndex maps SessionID -> Connect
	sessionIndex map[string]Connect
	// userRoutes tracks local user membership count and broker subscription state.
	userRoutes map[string]*subscriptionRoute
	// rooms maps RoomID -> []Connect
	rooms map[string]map[Connect]struct{}
	// roomRoutes tracks local room membership count and broker subscription state.
	roomRoutes map[string]*subscriptionRoute

	broker             MessageBroker
	channel            string
	nodeID             string
	ackChannel         string
	maxRoomsPerConnect int
	sendTimeout        time.Duration
	maxConcurrentSends int
	subscribeCancel    context.CancelFunc
	closeOnce          sync.Once
	closed             bool
	pendingMu          sync.Mutex
	pending            map[string]*pendingCommand
}

func NewHub(opts ...opt.Option[hubOptions]) (Hub, error) {
	options := &hubOptions{
		channel:            "ws:global",
		nodeID:             uuid.NewString(),
		maxRoomsPerConnect: 50,
		sendTimeout:        100 * time.Millisecond,
		maxConcurrentSends: 50,
	}
	opt.Each(options, opts...)
	if options.maxRoomsPerConnect <= 0 {
		options.maxRoomsPerConnect = 50
	}
	if options.sendTimeout <= 0 {
		options.sendTimeout = 100 * time.Millisecond
	}
	if options.maxConcurrentSends <= 0 {
		options.maxConcurrentSends = 50
	}

	registerWSMetrics()

	h := &hubEntity{
		connections:        make(map[Connect]struct{}),
		userIndex:          make(map[string]map[Connect]struct{}),
		sessionIndex:       make(map[string]Connect),
		userRoutes:         make(map[string]*subscriptionRoute),
		rooms:              make(map[string]map[Connect]struct{}),
		roomRoutes:         make(map[string]*subscriptionRoute),
		broker:             options.broker,
		channel:            options.channel,
		nodeID:             options.nodeID,
		ackChannel:         options.channel + ":ack:" + options.nodeID,
		maxRoomsPerConnect: options.maxRoomsPerConnect,
		sendTimeout:        options.sendTimeout,
		maxConcurrentSends: options.maxConcurrentSends,
		pending:            make(map[string]*pendingCommand),
	}

	if h.broker != nil {
		subscribeCtx, cancel := context.WithCancel(context.Background())
		if err := h.broker.Subscribe(subscribeCtx, h.channel, h.handleBrokerMessage); err != nil {
			cancel()
			return nil, err
		}
		if err := h.broker.Subscribe(subscribeCtx, h.ackChannel, h.handleAckMessage); err != nil {
			cancel()
			return nil, err
		}
		h.subscribeCancel = cancel
	}

	return h, nil
}

// hubOptions and Option helpers
type hubOptions struct {
	broker             MessageBroker
	channel            string
	nodeID             string
	maxRoomsPerConnect int
	sendTimeout        time.Duration
	maxConcurrentSends int
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

func WithHubNodeID(nodeID string) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) {
		o.nodeID = nodeID
	})
}

func WithHubMaxRoomsPerConnect(max int) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) {
		o.maxRoomsPerConnect = max
	})
}

func WithHubSendTimeout(timeout time.Duration) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) {
		o.sendTimeout = timeout
	})
}

func WithHubMaxConcurrentSends(max int) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) {
		o.maxConcurrentSends = max
	})
}

func (h *hubEntity) Register(c Connect) error {
	var (
		stale        []Connect
		userID       string
		userRoute    *subscriptionRoute
		activateUser bool
	)

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return errHubClosed
	}
	h.connections[c] = struct{}{}

	// Index by UserID if present
	uid := c.UserID()
	userID = uid
	if uid != "" {
		// 自动踢掉当前节点上该 UID 的旧连接 (单机挤号)
		if oldConns, ok := h.userIndex[uid]; ok {
			for oldC := range oldConns {
				if oldC.SessionID() != c.SessionID() {
					stale = append(stale, oldC)
				}
			}
		}

		if h.userIndex[uid] == nil {
			h.userIndex[uid] = make(map[Connect]struct{})
		}
		h.userIndex[uid][c] = struct{}{}
		if h.broker != nil {
			userRoute = h.userRoutes[uid]
			if userRoute == nil {
				userRoute = newSubscriptionRoute()
				h.userRoutes[uid] = userRoute
				activateUser = true
			}
			userRoute.refs++
		}
	}
	if sid := c.SessionID(); sid != "" {
		h.sessionIndex[sid] = c
	}
	h.mu.Unlock()

	for _, oldConn := range stale {
		_ = oldConn.Close()
	}
	if userRoute != nil {
		if activateUser {
			if err := h.activateUserRoute(userID, userRoute); err != nil {
				h.rollbackRegister(c)
				return err
			}
		} else if err := userRoute.wait(context.Background()); err != nil {
			h.rollbackRegister(c)
			return err
		}
	}
	return nil
}

func (h *hubEntity) Unregister(c Connect) {
	h.mu.Lock()
	if _, ok := h.connections[c]; ok {
		delete(h.connections, c)

		var routeCancels []context.CancelFunc

		// Remove from index
		uid := c.UserID()
		if uid != "" && h.userIndex[uid] != nil {
			delete(h.userIndex[uid], c)
			if len(h.userIndex[uid]) == 0 {
				delete(h.userIndex, uid)
			}
			if route := h.userRoutes[uid]; route != nil {
				if route.refs > 0 {
					route.refs--
				}
				if route.refs == 0 {
					delete(h.userRoutes, uid)
					if route.cancel != nil {
						routeCancels = append(routeCancels, route.cancel)
					}
				}
			}
		}
		if sid := c.SessionID(); sid != "" && h.sessionIndex[sid] == c {
			delete(h.sessionIndex, sid)
		}

		// Optimized removal from rooms
		if member, ok := c.(roomMember); ok {
			for _, room := range member.rooms() {
				if conns, ok := h.rooms[room]; ok {
					delete(conns, c)
					if len(conns) == 0 {
						delete(h.rooms, room)
					}
				}
				if route := h.roomRoutes[room]; route != nil {
					if route.refs > 0 {
						route.refs--
					}
					if route.refs == 0 {
						delete(h.roomRoutes, room)
						if route.cancel != nil {
							routeCancels = append(routeCancels, route.cancel)
						}
					}
				}
			}
		}
		h.mu.Unlock()
		for _, cancel := range routeCancels {
			cancel()
		}
	}
	h.mu.Unlock()
}

func (h *hubEntity) Kick(ctx context.Context, userID string) error {
	return h.KickWithOrigin(ctx, userID, "")
}

func (h *hubEntity) KickWithOrigin(ctx context.Context, userID string, originID string) error {
	ctx = normalizeContext(ctx)
	if err := h.ensureOpen(); err != nil {
		return err
	}
	if err := requireUserID(userID); err != nil {
		return err
	}
	hubKick.Inc()

	// Wrap in protocol
	hm := hubMessage{
		Type:     "kick",
		Target:   userID,
		OriginID: originID,
	}
	h.injectTrace(ctx, &hm)

	if h.broker != nil {
		return h.dispatchUserCommand(ctx, userID, hm)
	}

	// Local fallback
	return h.kickLocal(ctx, userID, originID)
}

func (h *hubEntity) Join(ctx context.Context, sessionID string, room string) error {
	ctx = normalizeContext(ctx)
	if err := h.ensureOpen(); err != nil {
		return err
	}
	if err := requireSessionAndRoom(sessionID, room); err != nil {
		return err
	}

	route, created, err := h.joinLocal(ctx, sessionID, room)
	if err != nil || route == nil {
		return err
	}
	if created {
		h.startRoomRoute(room, route)
	}
	if err := route.wait(ctx); err != nil {
		h.rollbackJoin(sessionID, room)
		return err
	}
	return nil
}

func (h *hubEntity) Leave(ctx context.Context, sessionID string, room string) error {
	ctx = normalizeContext(ctx)
	if err := h.ensureOpen(); err != nil {
		return err
	}
	if err := requireSessionAndRoom(sessionID, room); err != nil {
		return err
	}

	cancel, err := h.leaveLocal(ctx, sessionID, room)
	if cancel != nil {
		cancel()
	}
	return err
}

func (h *hubEntity) BroadcastToRoom(ctx context.Context, room string, msg []byte) error {
	ctx = normalizeContext(ctx)
	if err := h.ensureOpen(); err != nil {
		return err
	}
	if err := requireRoom(room); err != nil {
		return err
	}
	hubRoomOps.WithLabelValues("broadcast").Inc()

	if h.broker != nil {
		rm := roomMessage{
			Payload: msg,
		}
		h.injectRoomTrace(ctx, &rm)
		data, err := json.Marshal(rm)
		if err != nil {
			return err
		}
		return h.broker.Publish(ctx, h.roomChannel(room), data)
	}

	// Local fallback
	return h.broadcastToRoomLocal(ctx, room, msg)
}

func (h *hubEntity) Broadcast(ctx context.Context, msg []byte) error {
	ctx = normalizeContext(ctx)
	if err := h.ensureOpen(); err != nil {
		return err
	}
	hubBroadcast.Inc()

	// Wrap in protocol
	hm := hubMessage{
		Type:    "broadcast",
		Payload: msg,
	}
	h.injectTrace(ctx, &hm)

	if h.broker != nil {
		return h.dispatchDistributed(ctx, hm)
	}

	// Local fallback
	return h.broadcastLocal(ctx, msg)
}

func (h *hubEntity) SendTo(ctx context.Context, userID string, msg []byte) error {
	return h.SendToWithOrigin(ctx, userID, msg, "")
}

func (h *hubEntity) SendToWithOrigin(ctx context.Context, userID string, msg []byte, originID string) error {
	ctx = normalizeContext(ctx)
	if err := h.ensureOpen(); err != nil {
		return err
	}
	if err := requireUserID(userID); err != nil {
		return err
	}
	// Wrap in protocol
	hm := hubMessage{
		Type:     "unicast",
		Target:   userID,
		Payload:  msg,
		OriginID: originID,
	}
	h.injectTrace(ctx, &hm)

	if h.broker != nil {
		return h.dispatchUserCommand(ctx, userID, hm)
	}

	// Local fallback
	return h.sendToLocal(ctx, userID, msg, originID)
}

func (h *hubEntity) BroadcastJSON(ctx context.Context, v interface{}) error {
	ctx = normalizeContext(ctx)
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return h.Broadcast(ctx, data)
}

func (h *hubEntity) SendJSONTo(ctx context.Context, userID string, v interface{}) error {
	return h.SendJSONToWithOrigin(ctx, userID, v, "")
}

func (h *hubEntity) SendJSONToWithOrigin(ctx context.Context, userID string, v interface{}, originID string) error {
	ctx = normalizeContext(ctx)
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return h.SendToWithOrigin(ctx, userID, data, originID)
}

func (h *hubEntity) handleBrokerMessage(data []byte) {
	var hm hubMessage
	if err := json.Unmarshal(data, &hm); err != nil {
		return
	}

	// Extract Trace
	ctx := h.extractTrace(&hm)
	err := h.executeCommand(ctx, hm)
	if hm.RequestID != "" && hm.ReplyTo != "" && h.broker != nil {
		h.publishAck(ctx, hm, err)
	}
}

func (h *hubEntity) handleAckMessage(data []byte) {
	var hm hubMessage
	if err := json.Unmarshal(data, &hm); err != nil {
		return
	}
	if hm.Type != "ack" || hm.RequestID == "" || hm.NodeID == "" {
		return
	}

	h.pendingMu.Lock()
	pending := h.pending[hm.RequestID]
	h.pendingMu.Unlock()
	if pending == nil {
		return
	}

	pending.ack(hm.NodeID, hm.Error)
}

func (h *hubEntity) handleUserBrokerMessage(userID string, data []byte) {
	var hm hubMessage
	if err := json.Unmarshal(data, &hm); err != nil {
		return
	}

	if hm.Target == "" {
		hm.Target = userID
	}

	ctx := h.extractTrace(&hm)
	_ = h.executeCommand(ctx, hm)
}

func (h *hubEntity) handleRoomBrokerMessage(room string, data []byte) {
	var rm roomMessage
	if err := json.Unmarshal(data, &rm); err != nil {
		return
	}

	ctx := h.extractRoomTrace(&rm)
	_ = h.broadcastToRoomLocal(ctx, room, rm.Payload)
}

func (h *hubEntity) executeCommand(ctx context.Context, hm hubMessage) error {
	switch hm.Type {
	case "broadcast":
		return h.broadcastLocal(ctx, hm.Payload)
	case "unicast":
		return h.sendToLocal(ctx, hm.Target, hm.Payload, hm.OriginID)
	case "kick":
		return h.kickLocal(ctx, hm.Target, hm.OriginID)
	default:
		return fmt.Errorf("wsx: unsupported hub message type %q", hm.Type)
	}
}

func (h *hubEntity) dispatchDistributed(ctx context.Context, hm hubMessage) error {
	expected, err := h.broker.NumSubscribers(ctx, h.channel)
	if err != nil {
		return err
	}
	if expected == 0 {
		return nil
	}

	requestID := uuid.NewString()
	pending := newPendingCommand(expected)

	h.pendingMu.Lock()
	h.pending[requestID] = pending
	h.pendingMu.Unlock()
	defer h.deletePending(requestID)

	hm.RequestID = requestID
	hm.ReplyTo = h.ackChannel
	hm.NodeID = h.nodeID

	data, err := json.Marshal(hm)
	if err != nil {
		return err
	}
	if err := h.broker.Publish(ctx, h.channel, data); err != nil {
		return err
	}

	return pending.wait(ctx)
}

func (h *hubEntity) dispatchUserCommand(ctx context.Context, userID string, hm hubMessage) error {
	data, err := json.Marshal(hm)
	if err != nil {
		return err
	}
	return h.broker.Publish(ctx, h.userChannel(userID), data)
}

func (h *hubEntity) publishAck(ctx context.Context, hm hubMessage, execErr error) {
	ack := hubMessage{
		Type:      "ack",
		RequestID: hm.RequestID,
		ReplyTo:   hm.ReplyTo,
		NodeID:    h.nodeID,
	}
	if execErr != nil {
		ack.Error = execErr.Error()
	}

	data, err := json.Marshal(ack)
	if err != nil {
		return
	}
	_ = h.broker.Publish(ctx, hm.ReplyTo, data)
}

func (h *hubEntity) deletePending(requestID string) {
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()
	delete(h.pending, requestID)
}

// injectTrace adds current span context to hubMessage
func (h *hubEntity) injectTrace(ctx context.Context, hm *hubMessage) {
	hm.TraceHeader = injectTraceHeader(ctx, hm.TraceHeader)
}

func (h *hubEntity) injectRoomTrace(ctx context.Context, rm *roomMessage) {
	rm.TraceHeader = injectTraceHeader(ctx, rm.TraceHeader)
}

func injectTraceHeader(ctx context.Context, header map[string]string) map[string]string {
	ctx = normalizeContext(ctx)
	if header == nil {
		header = make(map[string]string)
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(header))
	return header
}

// extractTrace gets context from hubMessage
func (h *hubEntity) extractTrace(hm *hubMessage) context.Context {
	return extractTraceHeader(hm.TraceHeader)
}

func (h *hubEntity) extractRoomTrace(rm *roomMessage) context.Context {
	return extractTraceHeader(rm.TraceHeader)
}

func extractTraceHeader(header map[string]string) context.Context {
	if header == nil {
		return context.Background()
	}
	carrier := propagation.MapCarrier(header)
	return otel.GetTextMapPropagator().Extract(context.Background(), carrier)
}

func (h *hubEntity) broadcastLocal(ctx context.Context, msg []byte) error {
	h.mu.RLock()
	// Snapshot connections to avoid holding lock during send
	conns := make([]Connect, 0, len(h.connections))
	for c := range h.connections {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	return h.batchSend(ctx, conns, msg)
}

func (h *hubEntity) sendToLocal(ctx context.Context, userID string, msg []byte, originID string) error {
	h.mu.RLock()
	// Snapshot connections
	targetConns := h.userIndex[userID]
	conns := make([]Connect, 0, len(targetConns))
	for c := range targetConns {
		// 排除发起方 Session
		if originID == "" || c.SessionID() != originID {
			conns = append(conns, c)
		}
	}
	h.mu.RUnlock()

	return h.batchSend(ctx, conns, msg)
}

func (h *hubEntity) kickLocal(ctx context.Context, userID string, originID string) error {
	_ = ctx
	h.mu.RLock()
	targetConns := h.userIndex[userID]
	conns := make([]Connect, 0, len(targetConns))
	for c := range targetConns {
		// 只有当 SessionID 不等于 originID 时才踢掉，防止自杀
		if originID == "" || c.SessionID() != originID {
			conns = append(conns, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range conns {
		_ = c.Close()
	}
	return nil
}

func (h *hubEntity) broadcastToRoomLocal(ctx context.Context, room string, msg []byte) error {
	h.mu.RLock()
	targetConns := h.rooms[room]
	conns := make([]Connect, 0, len(targetConns))
	for c := range targetConns {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	return h.batchSend(ctx, conns, msg)
}

func (h *hubEntity) joinLocal(ctx context.Context, sessionID string, room string) (*subscriptionRoute, bool, error) {
	_ = ctx
	h.mu.Lock()
	defer h.mu.Unlock()

	conn := h.sessionIndex[sessionID]
	if conn == nil {
		return nil, false, errHubSessionNotFound
	}
	member, ok := conn.(roomMember)
	if !ok {
		return nil, false, errHubRoomMembershipUnsupported
	}

	if roomConns := h.rooms[room]; roomConns != nil {
		if _, exists := roomConns[conn]; exists {
			return nil, false, nil
		}
	}

	if member.roomCount() >= h.maxRoomsPerConnect {
		limitExceeded.WithLabelValues("max_rooms").Inc()
		return nil, false, fmt.Errorf("wsx: session %s exceeded max rooms", conn.SessionID())
	}

	if h.rooms[room] == nil {
		h.rooms[room] = make(map[Connect]struct{})
	}
	h.rooms[room][conn] = struct{}{}
	member.joinRoom(room)
	hubRoomOps.WithLabelValues("join").Inc()

	if h.broker == nil {
		return nil, false, nil
	}

	route := h.roomRoutes[room]
	created := false
	if route == nil {
		route = newSubscriptionRoute()
		h.roomRoutes[room] = route
		created = true
	}
	route.refs++
	return route, created, nil
}

func (h *hubEntity) leaveLocal(ctx context.Context, sessionID string, room string) (context.CancelFunc, error) {
	_ = ctx
	h.mu.Lock()
	defer h.mu.Unlock()

	conn := h.sessionIndex[sessionID]
	if conn == nil {
		return nil, errHubSessionNotFound
	}

	roomConns := h.rooms[room]
	if roomConns == nil {
		return nil, nil
	}

	if _, ok := roomConns[conn]; !ok {
		return nil, nil
	}

	delete(roomConns, conn)
	if member, ok := conn.(roomMember); ok {
		member.leaveRoom(room)
	}
	hubRoomOps.WithLabelValues("leave").Inc()

	if len(roomConns) == 0 {
		delete(h.rooms, room)
	}

	if h.broker == nil {
		return nil, nil
	}

	route := h.roomRoutes[room]
	if route == nil {
		return nil, nil
	}
	if route.refs > 0 {
		route.refs--
	}
	if route.refs > 0 {
		return nil, nil
	}

	delete(h.roomRoutes, room)
	return route.cancel, nil
}

func (h *hubEntity) batchSend(ctx context.Context, conns []Connect, msg []byte) error {
	ctx = normalizeContext(ctx)

	// For small number of connections, send sequentially to avoid goroutine overhead
	if len(conns) <= 10 {
		var sendErr error
		for _, c := range conns {
			sendErr = errors.Join(sendErr, h.sendWithTimeout(ctx, c, msg))
		}
		return sendErr
	}

	// For larger numbers, use a bit of concurrency
	// We use a semaphore-like approach or just simple goroutines if the number isn't extreme.
	// Considering SendBinary is non-blocking (it just pushes to a channel),
	// the main bottleneck would be many connections with full channels.

	var wg sync.WaitGroup
	var sendErr error
	var errMu sync.Mutex
	// Limit concurrency to avoid spawning too many goroutines at once
	sem := make(chan struct{}, h.maxConcurrentSends)

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

			if err := h.sendWithTimeout(ctx, conn, msg); err != nil {
				errMu.Lock()
				sendErr = errors.Join(sendErr, err)
				errMu.Unlock()
			}
		}(c)
	}
	wg.Wait()
	return sendErr
}

func (h *hubEntity) Count() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return int64(len(h.connections))
}

func (h *hubEntity) Close() {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		h.closed = true
		cancel := h.subscribeCancel
		routeCancels := make([]context.CancelFunc, 0, len(h.userRoutes)+len(h.roomRoutes))
		for _, route := range h.userRoutes {
			if route != nil && route.cancel != nil {
				routeCancels = append(routeCancels, route.cancel)
			}
		}
		for _, route := range h.roomRoutes {
			if route != nil && route.cancel != nil {
				routeCancels = append(routeCancels, route.cancel)
			}
		}
		// Snapshot to avoid holding lock during Close which might trigger callbacks or be slow
		conns := make([]Connect, 0, len(h.connections))
		for c := range h.connections {
			conns = append(conns, c)
		}
		// Clear maps immediately to prevent further operations
		h.connections = make(map[Connect]struct{})
		h.userIndex = make(map[string]map[Connect]struct{})
		h.sessionIndex = make(map[string]Connect)
		h.userRoutes = make(map[string]*subscriptionRoute)
		h.rooms = make(map[string]map[Connect]struct{})
		h.roomRoutes = make(map[string]*subscriptionRoute)
		h.mu.Unlock()

		h.pendingMu.Lock()
		pending := make([]*pendingCommand, 0, len(h.pending))
		for requestID, command := range h.pending {
			pending = append(pending, command)
			delete(h.pending, requestID)
		}
		h.pendingMu.Unlock()

		if cancel != nil {
			cancel()
		}
		for _, routeCancel := range routeCancels {
			routeCancel()
		}
		for _, command := range pending {
			command.fail(errHubClosed)
		}
		for _, c := range conns {
			_ = c.Close()
		}
	})
}

func (h *hubEntity) sendWithTimeout(ctx context.Context, conn Connect, msg []byte) error {
	if h.sendTimeout <= 0 {
		return conn.SendBinary(ctx, msg)
	}

	sendCtx, cancel := context.WithTimeout(ctx, h.sendTimeout)
	defer cancel()

	return conn.SendBinary(sendCtx, msg)
}

func (h *hubEntity) ensureOpen() error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		return errHubClosed
	}
	return nil
}

func (h *hubEntity) startRoomRoute(room string, route *subscriptionRoute) {
	go func() {
		err := h.broker.Subscribe(route.ctx, h.roomChannel(room), func(data []byte) {
			h.handleRoomBrokerMessage(room, data)
		})

		h.mu.Lock()
		route.err = err
		close(route.ready)
		if err != nil && h.roomRoutes[room] == route && route.refs == 0 {
			delete(h.roomRoutes, room)
		}
		h.mu.Unlock()
	}()
}

func (h *hubEntity) activateUserRoute(userID string, route *subscriptionRoute) error {
	err := h.broker.Subscribe(route.ctx, h.userChannel(userID), func(data []byte) {
		h.handleUserBrokerMessage(userID, data)
	})
	route.err = err
	close(route.ready)
	return err
}

func (h *hubEntity) rollbackRegister(c Connect) {
	var cancel context.CancelFunc

	h.mu.Lock()
	delete(h.connections, c)
	if sid := c.SessionID(); sid != "" && h.sessionIndex[sid] == c {
		delete(h.sessionIndex, sid)
	}
	if uid := c.UserID(); uid != "" {
		if conns := h.userIndex[uid]; conns != nil {
			delete(conns, c)
			if len(conns) == 0 {
				delete(h.userIndex, uid)
			}
		}
		if route := h.userRoutes[uid]; route != nil {
			if route.refs > 0 {
				route.refs--
			}
			if route.refs == 0 {
				delete(h.userRoutes, uid)
				cancel = route.cancel
			}
		}
	}
	h.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (h *hubEntity) rollbackJoin(sessionID string, room string) {
	cancel, err := h.leaveLocal(context.Background(), sessionID, room)
	if err == nil && cancel != nil {
		cancel()
	}
}

func (h *hubEntity) userChannel(userID string) string {
	return h.channel + ":user:" + userID
}

func (h *hubEntity) roomChannel(room string) string {
	return h.channel + ":room:" + room
}

func requireUserID(userID string) error {
	if userID == "" {
		return errHubUserIDMissing
	}
	return nil
}

func requireSessionAndRoom(sessionID string, room string) error {
	if sessionID == "" {
		return errHubSessionIDMissing
	}
	return requireRoom(room)
}

func requireRoom(room string) error {
	if room == "" {
		return errHubRoomMissing
	}
	return nil
}

type subscriptionRoute struct {
	refs   int
	ctx    context.Context
	cancel context.CancelFunc
	ready  chan struct{}
	err    error
}

func newSubscriptionRoute() *subscriptionRoute {
	ctx, cancel := context.WithCancel(context.Background())
	return &subscriptionRoute{
		ctx:    ctx,
		cancel: cancel,
		ready:  make(chan struct{}),
	}
}

func (r *subscriptionRoute) wait(ctx context.Context) error {
	if r == nil || r.ready == nil {
		return nil
	}

	ctx = normalizeContext(ctx)
	select {
	case <-r.ready:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type pendingCommand struct {
	expected int64

	mu       sync.Mutex
	received map[string]struct{}
	err      error
	done     chan struct{}
	once     sync.Once
}

func newPendingCommand(expected int64) *pendingCommand {
	return &pendingCommand{
		expected: expected,
		received: make(map[string]struct{}),
		done:     make(chan struct{}),
	}
}

func (p *pendingCommand) ack(nodeID string, ackErr string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.received[nodeID]; exists {
		return
	}
	p.received[nodeID] = struct{}{}
	if ackErr != "" {
		p.err = errors.Join(p.err, fmt.Errorf("%s: %s", nodeID, ackErr))
	}
	if int64(len(p.received)) >= p.expected {
		p.once.Do(func() {
			close(p.done)
		})
	}
}

func (p *pendingCommand) wait(ctx context.Context) error {
	select {
	case <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *pendingCommand) fail(err error) {
	p.mu.Lock()
	p.err = errors.Join(p.err, err)
	p.mu.Unlock()
	p.once.Do(func() {
		close(p.done)
	})
}
