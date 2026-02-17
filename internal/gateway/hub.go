package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/channel"
	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/onboarding"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/presence"
	"github.com/uncord-chat/uncord-server/internal/role"
	servercfg "github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// Hub is the central WebSocket connection registry and event distributor. It manages client connections, subscribes to
// gateway events via Valkey pub/sub, and dispatches events to connected clients with permission filtering.
type Hub struct {
	clients        map[uuid.UUID]*Client
	mu             sync.RWMutex
	rdb            *redis.Client
	cfg            *config.Config
	sessions       *SessionStore
	resolver       *permission.Resolver
	users          user.Repository
	server         servercfg.Repository
	channels       channel.Repository
	roles          role.Repository
	members        member.Repository
	presence       *presence.Store
	publisher      *Publisher
	onboardingRepo onboarding.Repository
	documentStore  *onboarding.DocumentStore
	log            zerolog.Logger
}

// NewHub creates a new gateway hub.
func NewHub(
	rdb *redis.Client,
	cfg *config.Config,
	sessions *SessionStore,
	resolver *permission.Resolver,
	users user.Repository,
	server servercfg.Repository,
	channels channel.Repository,
	roles role.Repository,
	members member.Repository,
	presenceStore *presence.Store,
	publisher *Publisher,
	onboardingRepo onboarding.Repository,
	documentStore *onboarding.DocumentStore,
	logger zerolog.Logger,
) *Hub {
	return &Hub{
		clients:        make(map[uuid.UUID]*Client),
		rdb:            rdb,
		cfg:            cfg,
		sessions:       sessions,
		resolver:       resolver,
		users:          users,
		server:         server,
		channels:       channels,
		roles:          roles,
		members:        members,
		presence:       presenceStore,
		publisher:      publisher,
		onboardingRepo: onboardingRepo,
		documentStore:  documentStore,
		log:            logger.With().Str("component", "gateway").Logger(),
	}
}

// Run subscribes to the gateway events pub/sub channel and dispatches events to connected clients. It blocks until the
// context is cancelled or the subscription fails.
func (h *Hub) Run(ctx context.Context) error {
	sub := h.rdb.Subscribe(ctx, eventsChannel)
	defer func() { _ = sub.Close() }()

	h.log.Info().Msg("Gateway hub subscribed to event channel")

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			h.handlePubSubEvent(ctx, msg.Payload)
		}
	}
}

// ServeWebSocket initialises a new client for an upgraded WebSocket connection. It sends the Hello frame and starts
// the client's read and write pumps.
func (h *Hub) ServeWebSocket(conn *websocket.Conn) {
	client := newClient(h, conn, h.log)

	hello, err := NewHelloFrame(h.cfg.GatewayHeartbeatIntervalMS)
	if err != nil {
		h.log.Error().Err(err).Msg("Failed to build Hello frame")
		_ = conn.Close()
		return
	}

	_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
	if err := conn.WriteMessage(websocket.TextMessage, hello); err != nil {
		h.log.Debug().Err(err).Msg("Failed to send Hello frame")
		_ = conn.Close()
		return
	}

	go client.writePump()
	client.readPump()
}

// register adds an authenticated client to the Hub. If the user already has an active connection, the old connection
// is displaced with an InvalidSession frame.
func (h *Hub) register(client *Client) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.clients) >= h.cfg.GatewayMaxConnections {
		return ErrMaxConnections
	}

	userID := client.UserID()
	if existing, ok := h.clients[userID]; ok {
		h.log.Debug().Stringer("user_id", userID).Msg("Displacing existing connection")
		if frame, err := NewInvalidSessionFrame(false); err == nil {
			existing.enqueue(frame)
		}
		existing.closeSend()
		delete(h.clients, userID)
	}

	h.clients[userID] = client
	h.log.Debug().Stringer("user_id", userID).Int("total", len(h.clients)).Msg("Client registered")
	return nil
}

// unregister removes a client from the Hub and persists its session for future resume.
func (h *Hub) unregister(client *Client) {
	h.mu.Lock()

	userID := client.UserID()
	current, ok := h.clients[userID]
	if !ok || current != client {
		h.mu.Unlock()
		return
	}
	delete(h.clients, userID)
	h.mu.Unlock()

	client.closeSend()

	if client.IsIdentified() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.sessions.Save(ctx, client.SessionID(), userID, client.currentSeq()); err != nil {
			h.log.Warn().Err(err).Stringer("user_id", userID).Msg("Failed to save session on disconnect")
		}

		if h.presence != nil {
			go h.delayedOffline(userID)
		}
	}

	h.log.Debug().Stringer("user_id", userID).Msg("Client unregistered")
}

// delayedOffline waits for the configured offline grace period then publishes an offline presence event if the user
// has not reconnected. The delay is controlled by GatewayOfflineDelayMS in the server configuration.
func (h *Hub) delayedOffline(userID uuid.UUID) {
	time.Sleep(time.Duration(h.cfg.GatewayOfflineDelayMS) * time.Millisecond)

	h.mu.RLock()
	_, reconnected := h.clients[userID]
	h.mu.RUnlock()

	if reconnected {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.presence.Delete(ctx, userID); err != nil {
		h.log.Warn().Err(err).Stringer("user_id", userID).Msg("Failed to delete presence on delayed offline")
	}
	h.publishPresence(ctx, userID, presence.StatusOffline)
}

// handleIdentify authenticates a client using a JWT token, assembles the READY payload, and registers the client.
func (h *Hub) handleIdentify(client *Client, token string) {
	claims, err := auth.ValidateAccessToken(token, h.cfg.JWTSecret, h.cfg.ServerURL)
	if err != nil {
		h.log.Debug().Err(err).Msg("Identify token validation failed")
		client.closeWithCode(CloseAuthFailed, "invalid token")
		return
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		client.closeWithCode(CloseAuthFailed, "invalid token subject")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyData, err := h.assembleReady(ctx, userID)
	if err != nil {
		h.log.Error().Err(err).Stringer("user_id", userID).Msg("Failed to assemble READY payload")
		client.closeWithCode(CloseUnknownError, "internal error")
		return
	}

	sessionID := NewSessionID()
	readyData.SessionID = sessionID

	client.mu.Lock()
	client.userID = userID
	client.sessionID = sessionID
	client.identified = true
	client.mu.Unlock()

	if err := h.register(client); err != nil {
		h.log.Warn().Err(err).Msg("Failed to register client")
		client.closeWithCode(CloseUnknownError, "registration failed")
		return
	}

	readyPayload, err := json.Marshal(readyData)
	if err != nil {
		h.log.Error().Err(err).Msg("Failed to marshal READY payload")
		return
	}

	seq := client.nextSeq()
	frame, err := NewDispatchFrame(seq, events.Ready, readyPayload)
	if err != nil {
		h.log.Error().Err(err).Msg("Failed to build READY frame")
		return
	}
	client.enqueue(frame)

	if h.presence != nil {
		if pErr := h.presence.Set(ctx, userID, presence.StatusOnline); pErr != nil {
			h.log.Warn().Err(pErr).Stringer("user_id", userID).Msg("Failed to set initial presence")
		} else {
			h.publishPresence(ctx, userID, presence.StatusOnline)
		}
	}

	h.log.Info().Stringer("user_id", userID).Str("session_id", sessionID).Msg("Client identified")
}

// handleResume restores a client's session from Valkey and replays missed events.
func (h *Hub) handleResume(client *Client, data models.ResumeData) {
	claims, err := auth.ValidateAccessToken(data.Token, h.cfg.JWTSecret, h.cfg.ServerURL)
	if err != nil {
		h.log.Debug().Err(err).Msg("Resume token validation failed")
		client.closeWithCode(CloseAuthFailed, "invalid token")
		return
	}

	tokenUserID, err := uuid.Parse(claims.Subject)
	if err != nil {
		client.closeWithCode(CloseAuthFailed, "invalid token subject")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := h.sessions.Load(ctx, data.SessionID)
	if err != nil {
		h.log.Debug().Err(err).Str("session_id", data.SessionID).Msg("Session not found for resume")
		if frame, fErr := NewInvalidSessionFrame(false); fErr == nil {
			client.enqueue(frame)
		}
		return
	}

	if session.UserID != tokenUserID {
		h.log.Debug().Msg("Resume user ID does not match token")
		if frame, fErr := NewInvalidSessionFrame(false); fErr == nil {
			client.enqueue(frame)
		}
		return
	}

	if data.Seq > session.LastSeq {
		h.log.Debug().Int64("client_seq", data.Seq).Int64("server_seq", session.LastSeq).
			Msg("Resume sequence ahead of server")
		if frame, fErr := NewInvalidSessionFrame(false); fErr == nil {
			client.enqueue(frame)
		}
		return
	}

	// Replay missed events.
	missed, err := h.sessions.Replay(ctx, data.SessionID, data.Seq)
	if err != nil {
		h.log.Warn().Err(err).Msg("Failed to load replay buffer")
		if frame, fErr := NewInvalidSessionFrame(false); fErr == nil {
			client.enqueue(frame)
		}
		return
	}

	client.mu.Lock()
	client.userID = tokenUserID
	client.sessionID = data.SessionID
	client.seq.Store(session.LastSeq)
	client.identified = true
	client.mu.Unlock()

	if err := h.register(client); err != nil {
		h.log.Warn().Err(err).Msg("Failed to register resumed client")
		client.closeWithCode(CloseUnknownError, "registration failed")
		return
	}

	// Clean up the persisted session now that the client is back.
	if err := h.sessions.Delete(ctx, data.SessionID); err != nil {
		h.log.Warn().Err(err).Msg("Failed to delete session after resume")
	}

	// Send missed events.
	for _, payload := range missed {
		client.enqueue(payload)
	}

	// Send RESUMED dispatch.
	seq := client.nextSeq()
	resumedData, _ := json.Marshal(struct{}{})
	frame, err := NewDispatchFrame(seq, events.Resumed, resumedData)
	if err != nil {
		h.log.Error().Err(err).Msg("Failed to build RESUMED frame")
		return
	}
	client.enqueue(frame)

	if h.presence != nil {
		status, gErr := h.presence.Get(ctx, tokenUserID)
		if gErr != nil {
			h.log.Warn().Err(gErr).Stringer("user_id", tokenUserID).Msg("Failed to get presence on resume")
		}
		if status == presence.StatusOffline {
			if pErr := h.presence.Set(ctx, tokenUserID, presence.StatusOnline); pErr != nil {
				h.log.Warn().Err(pErr).Stringer("user_id", tokenUserID).Msg("Failed to restore presence on resume")
			} else {
				h.publishPresence(ctx, tokenUserID, presence.StatusOnline)
			}
		} else {
			_ = h.presence.Refresh(ctx, tokenUserID)
		}
	}

	h.log.Info().Stringer("user_id", tokenUserID).Str("session_id", data.SessionID).
		Int("replayed", len(missed)).Msg("Client resumed")
}

// handlePresenceUpdate processes a client's opcode 3 presence update. It validates the status, stores it in Valkey,
// and publishes a PRESENCE_UPDATE dispatch. Invisible status is stored truthfully but broadcast as offline.
func (h *Hub) handlePresenceUpdate(client *Client, status string) {
	if h.presence == nil {
		return
	}

	userID := client.UserID()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.presence.Set(ctx, userID, status); err != nil {
		h.log.Warn().Err(err).Stringer("user_id", userID).Msg("Failed to set presence")
		return
	}

	broadcastStatus := status
	if status == presence.StatusInvisible {
		broadcastStatus = presence.StatusOffline
	}
	h.publishPresence(ctx, userID, broadcastStatus)
}

// publishPresence publishes a PRESENCE_UPDATE dispatch event to the gateway events channel.
func (h *Hub) publishPresence(ctx context.Context, userID uuid.UUID, status string) {
	if h.publisher == nil {
		return
	}
	data := models.PresenceUpdateData{
		UserID: userID.String(),
		Status: status,
	}
	if err := h.publisher.Publish(ctx, events.PresenceUpdate, data); err != nil {
		h.log.Warn().Err(err).Stringer("user_id", userID).Msg("Failed to publish presence update")
	}
}

// refreshPresence extends the TTL of the user's presence key without changing the stored status.
func (h *Hub) refreshPresence(ctx context.Context, userID uuid.UUID) {
	if h.presence == nil {
		return
	}
	if err := h.presence.Refresh(ctx, userID); err != nil {
		h.log.Debug().Err(err).Stringer("user_id", userID).Msg("Failed to refresh presence TTL")
	}
}

// ephemeralEvent returns true for dispatch event types that should be sent without a sequence number and not stored in
// the replay buffer.
func ephemeralEvent(eventType events.DispatchEvent) bool {
	return eventType == events.TypingStart || eventType == events.TypingStop
}

// channelScoped extracts the channel_id from an event payload for permission filtering.
type channelScoped struct {
	ChannelID string `json:"channel_id"`
}

// handlePubSubEvent processes a single event from the Valkey pub/sub channel and dispatches it to connected clients.
func (h *Hub) handlePubSubEvent(ctx context.Context, payload string) {
	var env envelope
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		h.log.Warn().Err(err).Msg("Invalid gateway event envelope")
		return
	}

	eventType := events.DispatchEvent(env.Type)

	// Re-marshal the data field to json.RawMessage for the frame constructor.
	rawData, err := json.Marshal(env.Data)
	if err != nil {
		h.log.Warn().Err(err).Msg("Failed to re-marshal event data")
		return
	}

	// Check if this is a channel-scoped event.
	var scoped channelScoped
	_ = json.Unmarshal(rawData, &scoped)

	var channelID uuid.UUID
	var isChannelScoped bool
	if scoped.ChannelID != "" {
		if parsed, pErr := uuid.Parse(scoped.ChannelID); pErr == nil {
			channelID = parsed
			isChannelScoped = true
		}
	}

	h.mu.RLock()
	targets := make([]*Client, 0, len(h.clients))
	for _, c := range h.clients {
		if c.IsIdentified() {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	if len(targets) == 0 {
		return
	}

	// For channel-scoped events, filter by ViewChannels permission.
	if isChannelScoped {
		userIDs := make([]uuid.UUID, len(targets))
		for i, c := range targets {
			userIDs[i] = c.UserID()
		}

		permitted := make([]*Client, 0, len(targets))
		for i, c := range targets {
			ok, pErr := h.resolver.HasPermission(ctx, userIDs[i], channelID, permissions.ViewChannels)
			if pErr != nil {
				h.log.Warn().Err(pErr).Stringer("user_id", userIDs[i]).Msg("Permission check failed during dispatch")
				continue
			}
			if ok {
				permitted = append(permitted, c)
			}
		}
		targets = permitted
	}

	// Ephemeral events (e.g. TYPING_START) are sent without a sequence number and are not stored in the replay buffer.
	if ephemeralEvent(eventType) {
		frame, fErr := NewEphemeralDispatchFrame(eventType, rawData)
		if fErr != nil {
			h.log.Warn().Err(fErr).Msg("Failed to build ephemeral dispatch frame")
			return
		}
		for _, c := range targets {
			c.enqueue(frame)
		}
		return
	}

	// Build and send a sequenced dispatch frame per client and append to the replay buffer.
	for _, c := range targets {
		seq := c.nextSeq()
		frame, fErr := NewDispatchFrame(seq, eventType, rawData)
		if fErr != nil {
			h.log.Warn().Err(fErr).Msg("Failed to build dispatch frame")
			continue
		}

		c.enqueue(frame)

		// Append to the replay buffer (best-effort). The session ID is only available for identified clients.
		if sid := c.SessionID(); sid != "" {
			if rErr := h.sessions.AppendReplay(ctx, sid, seq, frame); rErr != nil {
				h.log.Warn().Err(rErr).Str("session_id", sid).Msg("Failed to append to replay buffer")
			}
		}
	}
}

// assembleReady queries the database for all state needed by a newly connected client.
func (h *Hub) assembleReady(ctx context.Context, userID uuid.UUID) (*models.ReadyData, error) {
	u, err := h.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	srv, err := h.server.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("get server config: %w", err)
	}

	chs, err := h.channels.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}

	rs, err := h.roles.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}

	ms, err := h.members.List(ctx, nil, 1000)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}

	var presences []models.PresenceState
	if h.presence != nil {
		memberIDs := make([]uuid.UUID, len(ms))
		for i := range ms {
			memberIDs[i] = ms[i].UserID
		}
		presences, err = h.presence.GetMany(ctx, memberIDs)
		if err != nil {
			return nil, fmt.Errorf("get presences: %w", err)
		}
	}

	var onboardingCfg *models.OnboardingConfig
	if h.onboardingRepo != nil {
		cfg, oErr := h.onboardingRepo.Get(ctx)
		if oErr != nil {
			h.log.Warn().Err(oErr).Msg("Failed to load onboarding config for READY payload")
		} else {
			var docs []models.OnboardingDocument
			if h.documentStore != nil {
				docs = h.documentStore.ToModels()
			}
			m := cfg.ToModel(docs)
			onboardingCfg = &m
		}
	}

	return &models.ReadyData{
		User:       u.ToModel(),
		Server:     srv.ToModel(),
		Channels:   channelSliceToModels(chs),
		Roles:      roleSliceToModels(rs),
		Members:    memberSliceToModels(ms),
		Presences:  presences,
		Onboarding: onboardingCfg,
	}, nil
}

// Shutdown gracefully closes all active connections. It sends a Reconnect frame to each client, cleans up presence
// keys, and closes the underlying WebSocket with a Going Away status.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.presence != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for userID := range h.clients {
			_ = h.presence.Delete(ctx, userID)
		}
	}

	reconnect, _ := NewReconnectFrame()
	for userID, client := range h.clients {
		if reconnect != nil {
			client.enqueue(reconnect)
		}
		client.closeSend()
		_ = client.conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
			time.Now().Add(writeWait),
		)
		_ = client.conn.Close()
		delete(h.clients, userID)
	}
	h.log.Info().Msg("Gateway hub shut down")
}

// ClientCount returns the number of currently connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Slice conversion helpers that delegate to each domain type's ToModel() method.

func channelSliceToModels(chs []channel.Channel) []models.Channel {
	result := make([]models.Channel, len(chs))
	for i := range chs {
		result[i] = chs[i].ToModel()
	}
	return result
}

func roleSliceToModels(rs []role.Role) []models.Role {
	result := make([]models.Role, len(rs))
	for i := range rs {
		result[i] = rs[i].ToModel()
	}
	return result
}

func memberSliceToModels(ms []member.MemberWithProfile) []models.Member {
	result := make([]models.Member, len(ms))
	for i := range ms {
		result[i] = ms[i].ToModel()
	}
	return result
}
