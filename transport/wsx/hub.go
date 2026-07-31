package wsx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bang-go/opt"
	"github.com/coder/websocket"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type Hub interface {
	Register(context.Context, Connect) error
	Unregister(context.Context, Connect) error
	Broadcast(context.Context, []byte) error
	SendToUser(context.Context, string, []byte) error
	BroadcastToRoom(context.Context, string, []byte) error
	DisconnectUser(context.Context, string, []byte) error
	DisconnectRoom(context.Context, string, []byte) error
	JoinSessionToRoom(context.Context, string, string) error
	JoinUserToRoom(context.Context, string, string) error
	LeaveUserFromRoom(context.Context, string, string) error
	start(context.Context) error
	shutdown(context.Context) error
}

const (
	commandBroadcast      = "broadcast"
	commandSendUser       = "send_user"
	commandDisconnectUser = "disconnect_user"
	commandJoinUserRoom   = "join_user_room"
	commandLeaveUserRoom  = "leave_user_room"
	commandBroadcastRoom  = "broadcast_room"
	commandDisconnectRoom = "disconnect_room"
)

type brokerCommand struct {
	Type        string            `json:"type"`
	RequestID   string            `json:"request_id,omitempty"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	NodeID      string            `json:"node_id,omitempty"`
	Target      string            `json:"target,omitempty"`
	Room        string            `json:"room,omitempty"`
	Payload     []byte            `json:"payload,omitempty"`
	TraceHeader map[string]string `json:"trace_header,omitempty"`
	Error       string            `json:"error,omitempty"`
}

type hubEntity struct {
	mu          sync.RWMutex
	started     bool
	closed      bool
	connections map[Connect]struct{}
	userIndex   map[string]map[Connect]struct{}
	sessions    map[string]Connect
	rooms       map[string]map[Connect]struct{}
	userRoutes  map[string]*subscriptionRoute
	roomRoutes  map[string]*subscriptionRoute

	broker             MessageBroker
	channel            string
	nodeID             string
	ackChannel         string
	maxRoomsPerConnect int
	commandTimeout     time.Duration
	maxConcurrentSends int
	globalSubscription Subscription
	ackSubscription    Subscription

	pendingMu sync.Mutex
	pending   map[string]*pendingCommand

	shutdownMu  sync.Mutex
	shutdownErr error
}

func NewHub(broker MessageBroker, opts ...opt.Option[hubOptions]) (Hub, error) {
	options := &hubOptions{
		channel:            "ws:global",
		nodeID:             uuid.NewString(),
		maxRoomsPerConnect: 50,
		commandTimeout:     3 * time.Second,
		maxConcurrentSends: 50,
	}
	opt.Each(options, opts...)
	if options.channel == "" {
		return nil, fmt.Errorf("wsx: hub channel is required")
	}
	if options.nodeID == "" {
		return nil, fmt.Errorf("wsx: hub node id is required")
	}
	if options.maxRoomsPerConnect <= 0 || options.commandTimeout <= 0 || options.maxConcurrentSends <= 0 {
		return nil, fmt.Errorf("wsx: hub limits and timeouts must be positive")
	}
	registerWSMetrics()
	return &hubEntity{
		connections:        make(map[Connect]struct{}),
		userIndex:          make(map[string]map[Connect]struct{}),
		sessions:           make(map[string]Connect),
		rooms:              make(map[string]map[Connect]struct{}),
		userRoutes:         make(map[string]*subscriptionRoute),
		roomRoutes:         make(map[string]*subscriptionRoute),
		broker:             broker,
		channel:            options.channel,
		nodeID:             options.nodeID,
		ackChannel:         options.channel + ":ack:" + options.nodeID,
		maxRoomsPerConnect: options.maxRoomsPerConnect,
		commandTimeout:     options.commandTimeout,
		maxConcurrentSends: options.maxConcurrentSends,
		pending:            make(map[string]*pendingCommand),
	}, nil
}

func (h *hubEntity) start(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return ErrHubClosed
	}
	if h.started {
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	if h.broker != nil {
		global, err := h.broker.Subscribe(ctx, h.channel, h.handleCommand)
		if err != nil {
			return err
		}
		ack, err := h.broker.Subscribe(ctx, h.ackChannel, h.handleAck)
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), h.commandTimeout)
			cleanupErr := global.Close(cleanupCtx)
			cancel()
			return errors.Join(err, cleanupErr)
		}
		h.mu.Lock()
		h.globalSubscription = global
		h.ackSubscription = ack
		h.mu.Unlock()
	}
	h.mu.Lock()
	h.started = true
	h.mu.Unlock()
	return nil
}

func (h *hubEntity) Register(ctx context.Context, conn Connect) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if conn == nil {
		return ErrConnectionRequired
	}
	if err := requireSessionID(conn.SessionID()); err != nil {
		return err
	}

	var route *subscriptionRoute
	var createRoute bool
	h.mu.Lock()
	if err := h.ensureRunningLocked(); err != nil {
		h.mu.Unlock()
		return err
	}
	if _, exists := h.connections[conn]; exists || h.sessions[conn.SessionID()] != nil {
		h.mu.Unlock()
		return ErrSessionDuplicate
	}
	h.connections[conn] = struct{}{}
	h.sessions[conn.SessionID()] = conn
	if userID := conn.UserID(); userID != "" {
		if h.userIndex[userID] == nil {
			h.userIndex[userID] = make(map[Connect]struct{})
		}
		h.userIndex[userID][conn] = struct{}{}
		if h.broker != nil {
			route = h.userRoutes[userID]
			if route == nil {
				route = newSubscriptionRoute()
				h.userRoutes[userID] = route
				createRoute = true
			}
			route.refs++
		}
	}
	h.mu.Unlock()

	if route == nil {
		return nil
	}
	if createRoute {
		subscription, err := h.broker.Subscribe(ctx, h.userChannel(conn.UserID()), func(data []byte) {
			h.handleUserCommand(conn.UserID(), data)
		})
		h.completeRoute(route, subscription, err)
		if err != nil {
			return errors.Join(err, h.rollbackRegister(conn))
		}
		return nil
	}
	if err := route.wait(ctx); err != nil {
		return errors.Join(err, h.rollbackRegister(conn))
	}
	return nil
}

func (h *hubEntity) Unregister(ctx context.Context, conn Connect) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if conn == nil {
		return ErrConnectionRequired
	}
	var subscriptions []Subscription
	h.mu.Lock()
	if _, exists := h.connections[conn]; !exists {
		h.mu.Unlock()
		return nil
	}
	delete(h.connections, conn)
	delete(h.sessions, conn.SessionID())
	if userID := conn.UserID(); userID != "" {
		delete(h.userIndex[userID], conn)
		if len(h.userIndex[userID]) == 0 {
			delete(h.userIndex, userID)
		}
		if route := h.userRoutes[userID]; route != nil {
			route.refs--
			if route.refs == 0 {
				delete(h.userRoutes, userID)
				if route.subscription != nil {
					subscriptions = append(subscriptions, route.subscription)
				}
			}
		}
	}
	if member, ok := conn.(roomMember); ok {
		for _, room := range member.rooms() {
			delete(h.rooms[room], conn)
			if len(h.rooms[room]) == 0 {
				delete(h.rooms, room)
			}
			member.leaveRoom(room)
			if route := h.roomRoutes[room]; route != nil {
				route.refs--
				if route.refs == 0 {
					delete(h.roomRoutes, room)
					if route.subscription != nil {
						subscriptions = append(subscriptions, route.subscription)
					}
				}
			}
		}
	}
	h.mu.Unlock()
	return closeSubscriptions(ctx, subscriptions)
}

func (h *hubEntity) Broadcast(ctx context.Context, payload []byte) error {
	return h.dispatch(ctx, h.channel, brokerCommand{Type: commandBroadcast, Payload: payload})
}

func (h *hubEntity) SendToUser(ctx context.Context, userID string, payload []byte) error {
	if err := requireUserID(userID); err != nil {
		return err
	}
	return h.dispatch(ctx, h.userChannel(userID), brokerCommand{Type: commandSendUser, Target: userID, Payload: payload})
}

func (h *hubEntity) BroadcastToRoom(ctx context.Context, room string, payload []byte) error {
	if err := requireRoom(room); err != nil {
		return err
	}
	return h.dispatch(ctx, h.roomChannel(room), brokerCommand{Type: commandBroadcastRoom, Room: room, Payload: payload})
}

func (h *hubEntity) DisconnectUser(ctx context.Context, userID string, payload []byte) error {
	if err := requireUserID(userID); err != nil {
		return err
	}
	return h.dispatch(ctx, h.userChannel(userID), brokerCommand{Type: commandDisconnectUser, Target: userID, Payload: payload})
}

func (h *hubEntity) DisconnectRoom(ctx context.Context, room string, payload []byte) error {
	if err := requireRoom(room); err != nil {
		return err
	}
	return h.dispatch(ctx, h.roomChannel(room), brokerCommand{Type: commandDisconnectRoom, Room: room, Payload: payload})
}

func (h *hubEntity) JoinSessionToRoom(ctx context.Context, sessionID, room string) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := requireSessionID(sessionID); err != nil {
		return err
	}
	if err := requireRoom(room); err != nil {
		return err
	}
	h.mu.RLock()
	conn := h.sessions[sessionID]
	h.mu.RUnlock()
	if conn == nil {
		return ErrSessionNotFound
	}
	return h.joinConnectionsToRoom(ctx, []Connect{conn}, room)
}

func (h *hubEntity) JoinUserToRoom(ctx context.Context, userID, room string) error {
	if err := requireUserID(userID); err != nil {
		return err
	}
	if err := requireRoom(room); err != nil {
		return err
	}
	return h.dispatch(ctx, h.userChannel(userID), brokerCommand{Type: commandJoinUserRoom, Target: userID, Room: room})
}

func (h *hubEntity) LeaveUserFromRoom(ctx context.Context, userID, room string) error {
	if err := requireUserID(userID); err != nil {
		return err
	}
	if err := requireRoom(room); err != nil {
		return err
	}
	return h.dispatch(ctx, h.userChannel(userID), brokerCommand{Type: commandLeaveUserRoom, Target: userID, Room: room})
}

func (h *hubEntity) dispatch(ctx context.Context, channel string, command brokerCommand) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := h.ensureRunning(); err != nil {
		return err
	}
	commandCtx, cancel := h.commandContext(ctx)
	defer cancel()
	h.injectTrace(commandCtx, &command)
	if h.broker == nil {
		return h.execute(commandCtx, command)
	}

	requestID := uuid.NewString()
	pending := newPendingCommand()
	h.pendingMu.Lock()
	h.pending[requestID] = pending
	h.pendingMu.Unlock()
	defer h.deletePending(requestID)

	command.RequestID = requestID
	command.ReplyTo = h.ackChannel
	command.NodeID = h.nodeID
	data, err := json.Marshal(command)
	if err != nil {
		return err
	}
	expected, err := h.broker.Publish(commandCtx, channel, data)
	if err != nil {
		return err
	}
	pending.expect(expected)
	return pending.wait(commandCtx)
}

func (h *hubEntity) handleCommand(data []byte) { h.handleBoundCommand("", "", data) }
func (h *hubEntity) handleUserCommand(userID string, data []byte) {
	h.handleBoundCommand(userID, "", data)
}
func (h *hubEntity) handleRoomCommand(room string, data []byte) { h.handleBoundCommand("", room, data) }

func (h *hubEntity) handleBoundCommand(userID, room string, data []byte) {
	var command brokerCommand
	if err := json.Unmarshal(data, &command); err != nil {
		return
	}
	if command.Target == "" {
		command.Target = userID
	}
	if command.Room == "" {
		command.Room = room
	}
	ctx, cancel := context.WithTimeout(h.extractTrace(command.TraceHeader), h.commandTimeout)
	err := h.execute(ctx, command)
	cancel()
	if command.RequestID != "" && command.ReplyTo != "" {
		h.publishAck(command, err)
	}
}

func (h *hubEntity) execute(ctx context.Context, command brokerCommand) error {
	switch command.Type {
	case commandBroadcast:
		return h.sendConnections(ctx, h.snapshotAll(), command.Payload, false)
	case commandSendUser:
		return h.sendConnections(ctx, h.snapshotUser(command.Target), command.Payload, false)
	case commandDisconnectUser:
		return h.sendConnections(ctx, h.snapshotUser(command.Target), command.Payload, true)
	case commandJoinUserRoom:
		return h.joinConnectionsToRoom(ctx, h.snapshotUser(command.Target), command.Room)
	case commandLeaveUserRoom:
		return h.leaveConnectionsFromRoom(ctx, h.snapshotUser(command.Target), command.Room)
	case commandBroadcastRoom:
		return h.sendConnections(ctx, h.snapshotRoom(command.Room), command.Payload, false)
	case commandDisconnectRoom:
		return h.sendConnections(ctx, h.snapshotRoom(command.Room), command.Payload, true)
	default:
		return fmt.Errorf("wsx: unsupported broker command %q", command.Type)
	}
}

func (h *hubEntity) joinConnectionsToRoom(ctx context.Context, conns []Connect, room string) error {
	if len(conns) == 0 {
		return nil
	}
	var route *subscriptionRoute
	var createRoute bool
	joined := make([]Connect, 0, len(conns))
	h.mu.Lock()
	if err := h.ensureRunningLocked(); err != nil {
		h.mu.Unlock()
		return err
	}
	for _, conn := range conns {
		member, ok := conn.(roomMember)
		if !ok {
			h.mu.Unlock()
			return ErrRoomMembershipUnsupported
		}
		if _, exists := h.connections[conn]; !exists {
			continue
		}
		if _, exists := h.rooms[room][conn]; exists {
			continue
		}
		if member.roomCount() >= h.maxRoomsPerConnect {
			h.mu.Unlock()
			limitExceeded.WithLabelValues("max_rooms").Inc()
			return ErrRoomLimitExceeded
		}
	}
	if h.rooms[room] == nil {
		h.rooms[room] = make(map[Connect]struct{})
	}
	for _, conn := range conns {
		member, ok := conn.(roomMember)
		if !ok {
			continue
		}
		if _, registered := h.connections[conn]; !registered {
			continue
		}
		if _, exists := h.rooms[room][conn]; exists {
			continue
		}
		h.rooms[room][conn] = struct{}{}
		member.joinRoom(room)
		joined = append(joined, conn)
	}
	if h.broker != nil && len(joined) > 0 {
		route = h.roomRoutes[room]
		if route == nil {
			route = newSubscriptionRoute()
			h.roomRoutes[room] = route
			createRoute = true
		}
		route.refs += len(joined)
	}
	h.mu.Unlock()
	if len(joined) == 0 || route == nil {
		return nil
	}
	if createRoute {
		subscription, err := h.broker.Subscribe(ctx, h.roomChannel(room), func(data []byte) {
			h.handleRoomCommand(room, data)
		})
		h.completeRoute(route, subscription, err)
		if err != nil {
			return errors.Join(err, h.rollbackRoomJoin(joined, room))
		}
		return nil
	}
	if err := route.wait(ctx); err != nil {
		return errors.Join(err, h.rollbackRoomJoin(joined, room))
	}
	return nil
}

func (h *hubEntity) leaveConnectionsFromRoom(ctx context.Context, conns []Connect, room string) error {
	var subscription Subscription
	h.mu.Lock()
	removed := 0
	for _, conn := range conns {
		if _, exists := h.rooms[room][conn]; !exists {
			continue
		}
		delete(h.rooms[room], conn)
		if member, ok := conn.(roomMember); ok {
			member.leaveRoom(room)
		}
		removed++
	}
	if len(h.rooms[room]) == 0 {
		delete(h.rooms, room)
	}
	if route := h.roomRoutes[room]; route != nil {
		route.refs -= removed
		if route.refs <= 0 {
			delete(h.roomRoutes, room)
			subscription = route.subscription
		}
	}
	h.mu.Unlock()
	if subscription != nil {
		return subscription.Close(ctx)
	}
	return nil
}

func (h *hubEntity) rollbackRoomJoin(conns []Connect, room string) error {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), h.commandTimeout)
	err := h.leaveConnectionsFromRoom(rollbackCtx, conns, room)
	cancel()
	return err
}

func (h *hubEntity) sendConnections(ctx context.Context, conns []Connect, payload []byte, disconnect bool) error {
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var result error
	sem := make(chan struct{}, h.maxConcurrentSends)
	for _, conn := range conns {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			errMu.Lock()
			result = errors.Join(result, ctx.Err())
			errMu.Unlock()
			return result
		}
		wg.Add(1)
		go func(conn Connect) {
			defer wg.Done()
			defer func() { <-sem }()
			var err error
			if len(payload) > 0 {
				err = conn.WriteMessage(ctx, websocket.MessageBinary, payload)
			}
			if disconnect {
				err = errors.Join(err, conn.CloseNow())
			}
			if err != nil {
				errMu.Lock()
				result = errors.Join(result, err)
				errMu.Unlock()
			}
		}(conn)
	}
	wg.Wait()
	return result
}

func (h *hubEntity) shutdown(ctx context.Context) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	h.shutdownMu.Lock()
	h.mu.Lock()
	if !h.closed {
		h.closed = true
		subs := make([]Subscription, 0, 2+len(h.userRoutes)+len(h.roomRoutes))
		if h.globalSubscription != nil {
			subs = append(subs, h.globalSubscription)
		}
		if h.ackSubscription != nil {
			subs = append(subs, h.ackSubscription)
		}
		for _, route := range h.userRoutes {
			if route.subscription != nil {
				subs = append(subs, route.subscription)
			}
		}
		for _, route := range h.roomRoutes {
			if route.subscription != nil {
				subs = append(subs, route.subscription)
			}
		}
		h.mu.Unlock()
		h.shutdownErr = errors.Join(h.shutdownErr, closeSubscriptions(ctx, subs))
		h.pendingMu.Lock()
		for id, pending := range h.pending {
			pending.fail(ErrHubClosed)
			delete(h.pending, id)
		}
		h.pendingMu.Unlock()
		if h.broker != nil {
			h.shutdownErr = errors.Join(h.shutdownErr, h.broker.Shutdown(ctx))
		}
	} else {
		h.mu.Unlock()
	}
	err := h.shutdownErr
	h.shutdownMu.Unlock()
	return err
}

func (h *hubEntity) rollbackRegister(conn Connect) error {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), h.commandTimeout)
	err := h.Unregister(rollbackCtx, conn)
	cancel()
	return err
}

func (h *hubEntity) completeRoute(route *subscriptionRoute, subscription Subscription, err error) {
	h.mu.Lock()
	route.subscription = subscription
	route.err = err
	close(route.ready)
	h.mu.Unlock()
}

func (h *hubEntity) snapshotAll() []Connect {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]Connect, 0, len(h.connections))
	for conn := range h.connections {
		result = append(result, conn)
	}
	return result
}

func (h *hubEntity) snapshotUser(userID string) []Connect {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]Connect, 0, len(h.userIndex[userID]))
	for conn := range h.userIndex[userID] {
		result = append(result, conn)
	}
	return result
}

func (h *hubEntity) snapshotRoom(room string) []Connect {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]Connect, 0, len(h.rooms[room]))
	for conn := range h.rooms[room] {
		result = append(result, conn)
	}
	return result
}

func (h *hubEntity) ensureRunning() error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.ensureRunningLocked()
}

func (h *hubEntity) ensureRunningLocked() error {
	if h.closed {
		return ErrHubClosed
	}
	if !h.started {
		return ErrHubNotStarted
	}
	return nil
}

func (h *hubEntity) commandContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, h.commandTimeout)
}

func (h *hubEntity) publishAck(command brokerCommand, execErr error) {
	ack := brokerCommand{RequestID: command.RequestID, NodeID: h.nodeID}
	if execErr != nil {
		ack.Error = execErr.Error()
	}
	data, err := json.Marshal(ack)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.commandTimeout)
	_, _ = h.broker.Publish(ctx, command.ReplyTo, data)
	cancel()
}

func (h *hubEntity) handleAck(data []byte) {
	var ack brokerCommand
	if json.Unmarshal(data, &ack) != nil {
		return
	}
	h.pendingMu.Lock()
	pending := h.pending[ack.RequestID]
	h.pendingMu.Unlock()
	if pending != nil {
		pending.ack(ack.NodeID, ack.Error)
	}
}

func (h *hubEntity) deletePending(id string) {
	h.pendingMu.Lock()
	delete(h.pending, id)
	h.pendingMu.Unlock()
}

func (h *hubEntity) injectTrace(ctx context.Context, command *brokerCommand) {
	carrier := propagation.MapCarrier(command.TraceHeader)
	if carrier == nil {
		carrier = propagation.MapCarrier{}
	}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if len(carrier) > 0 {
		command.TraceHeader = map[string]string(carrier)
	}
}

func (h *hubEntity) extractTrace(header map[string]string) context.Context {
	if len(header) == 0 {
		return context.Background()
	}
	return otel.GetTextMapPropagator().Extract(context.Background(), propagation.MapCarrier(header))
}

func (h *hubEntity) userChannel(userID string) string { return h.channel + ":user:" + userID }
func (h *hubEntity) roomChannel(room string) string   { return h.channel + ":room:" + room }

func closeSubscriptions(ctx context.Context, subscriptions []Subscription) error {
	var result error
	for _, subscription := range subscriptions {
		result = errors.Join(result, subscription.Close(ctx))
	}
	return result
}

func requireUserID(userID string) error {
	if userID == "" {
		return ErrUserIDRequired
	}
	return nil
}
func requireSessionID(sessionID string) error {
	if sessionID == "" {
		return ErrSessionIDRequired
	}
	return nil
}
func requireRoom(room string) error {
	if room == "" {
		return ErrRoomRequired
	}
	return nil
}

type subscriptionRoute struct {
	refs         int
	ready        chan struct{}
	subscription Subscription
	err          error
}

func newSubscriptionRoute() *subscriptionRoute { return &subscriptionRoute{ready: make(chan struct{})} }

func (r *subscriptionRoute) wait(ctx context.Context) error {
	select {
	case <-r.ready:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type pendingCommand struct {
	mu        sync.Mutex
	expected  int64
	expectSet bool
	received  map[string]struct{}
	err       error
	done      chan struct{}
	once      sync.Once
}

func newPendingCommand() *pendingCommand {
	return &pendingCommand{received: make(map[string]struct{}), done: make(chan struct{})}
}

func (p *pendingCommand) expect(expected int64) {
	p.mu.Lock()
	p.expected = expected
	p.expectSet = true
	p.completeLocked()
	p.mu.Unlock()
}

func (p *pendingCommand) ack(nodeID, message string) {
	p.mu.Lock()
	if _, exists := p.received[nodeID]; !exists {
		p.received[nodeID] = struct{}{}
		if message != "" {
			p.err = errors.Join(p.err, fmt.Errorf("%s: %s", nodeID, message))
		}
	}
	p.completeLocked()
	p.mu.Unlock()
}

func (p *pendingCommand) completeLocked() {
	if p.expectSet && int64(len(p.received)) >= p.expected {
		p.once.Do(func() { close(p.done) })
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
	p.once.Do(func() { close(p.done) })
}
