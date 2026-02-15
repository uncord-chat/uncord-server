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
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/role"
	servercfg "github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// Hub is the central WebSocket connection registry and event distributor. It manages client connections, subscribes to
// gateway events via Valkey pub/sub, and dispatches events to connected clients with permission filtering.
type Hub struct {
	clients  map[uuid.UUID]*Client
	mu       sync.RWMutex
	rdb      *redis.Client
	cfg      *config.Config
	sessions *SessionStore
	resolver *permission.Resolver
	users    user.Repository
	server   servercfg.Repository
	channels channel.Repository
	roles    role.Repository
	members  member.Repository
	log      zerolog.Logger
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
	logger zerolog.Logger,
) *Hub {
	return &Hub{
		clients:  make(map[uuid.UUID]*Client),
		rdb:      rdb,
		cfg:      cfg,
		sessions: sessions,
		resolver: resolver,
		users:    users,
		server:   server,
		channels: channels,
		roles:    roles,
		members:  members,
		log:      logger.With().Str("component", "gateway").Logger(),
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
		close(existing.send)
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

	close(client.send)

	if client.IsIdentified() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.sessions.Save(ctx, client.SessionID(), userID, client.currentSeq()); err != nil {
			h.log.Warn().Err(err).Stringer("user_id", userID).Msg("Failed to save session on disconnect")
		}
	}

	h.log.Debug().Stringer("user_id", userID).Msg("Client unregistered")
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
	h.log.Info().Stringer("user_id", tokenUserID).Str("session_id", data.SessionID).
		Int("replayed", len(missed)).Msg("Client resumed")
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

	// Build and send a dispatch frame per client (each with its own sequence number) and append to the replay buffer.
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

	return &models.ReadyData{
		User:     toUserModel(u),
		Server:   toServerConfigModel(srv),
		Channels: toChannelModels(chs),
		Roles:    toRoleModels(rs),
		Members:  toMemberModels(ms),
	}, nil
}

// Shutdown gracefully closes all active connections. It sends a Reconnect frame to each client and closes the
// underlying WebSocket with a Going Away status.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	reconnect, _ := NewReconnectFrame()
	for userID, client := range h.clients {
		if reconnect != nil {
			client.enqueue(reconnect)
		}
		close(client.send)
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

// Model conversion functions. These mirror the unexported converters in the api package (e.g., api/user.go:toUserModel)
// and exist here because those are package-private.

func toUserModel(u *user.User) models.User {
	return models.User{
		ID:                   u.ID.String(),
		Email:                u.Email,
		Username:             u.Username,
		DisplayName:          u.DisplayName,
		AvatarKey:            u.AvatarKey,
		Pronouns:             u.Pronouns,
		BannerKey:            u.BannerKey,
		About:                u.About,
		ThemeColourPrimary:   u.ThemeColourPrimary,
		ThemeColourSecondary: u.ThemeColourSecondary,
		MFAEnabled:           u.MFAEnabled,
		EmailVerified:        u.EmailVerified,
	}
}

func toServerConfigModel(cfg *servercfg.Config) models.ServerConfig {
	return models.ServerConfig{
		ID:          cfg.ID.String(),
		Name:        cfg.Name,
		Description: cfg.Description,
		IconKey:     cfg.IconKey,
		BannerKey:   cfg.BannerKey,
		OwnerID:     cfg.OwnerID.String(),
		CreatedAt:   cfg.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   cfg.UpdatedAt.Format(time.RFC3339),
	}
}

func toChannelModels(chs []channel.Channel) []models.Channel {
	result := make([]models.Channel, len(chs))
	for i := range chs {
		result[i] = toChannelModel(&chs[i])
	}
	return result
}

func toChannelModel(ch *channel.Channel) models.Channel {
	var categoryID *string
	if ch.CategoryID != nil {
		s := ch.CategoryID.String()
		categoryID = &s
	}
	return models.Channel{
		ID:              ch.ID.String(),
		CategoryID:      categoryID,
		Name:            ch.Name,
		Type:            ch.Type,
		Topic:           ch.Topic,
		Position:        ch.Position,
		SlowmodeSeconds: ch.SlowmodeSeconds,
		NSFW:            ch.NSFW,
		CreatedAt:       ch.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       ch.UpdatedAt.Format(time.RFC3339),
	}
}

func toRoleModels(rs []role.Role) []models.Role {
	result := make([]models.Role, len(rs))
	for i := range rs {
		result[i] = toRoleModel(&rs[i])
	}
	return result
}

func toRoleModel(r *role.Role) models.Role {
	return models.Role{
		ID:          r.ID.String(),
		Name:        r.Name,
		Colour:      r.Colour,
		Position:    r.Position,
		Hoist:       r.Hoist,
		Permissions: r.Permissions,
		IsEveryone:  r.IsEveryone,
		CreatedAt:   r.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   r.UpdatedAt.Format(time.RFC3339),
	}
}

func toMemberModels(ms []member.MemberWithProfile) []models.Member {
	result := make([]models.Member, len(ms))
	for i := range ms {
		result[i] = toMemberModel(&ms[i])
	}
	return result
}

func toMemberModel(m *member.MemberWithProfile) models.Member {
	roleIDs := make([]string, len(m.RoleIDs))
	for i, id := range m.RoleIDs {
		roleIDs[i] = id.String()
	}
	result := models.Member{
		User: models.MemberUser{
			ID:          m.UserID.String(),
			Username:    m.Username,
			DisplayName: m.DisplayName,
			AvatarKey:   m.AvatarKey,
		},
		Nickname: m.Nickname,
		JoinedAt: m.JoinedAt.Format(time.RFC3339),
		Roles:    roleIDs,
		Status:   m.Status,
	}
	if m.TimeoutUntil != nil {
		s := m.TimeoutUntil.Format(time.RFC3339)
		result.TimeoutUntil = &s
	}
	return result
}
