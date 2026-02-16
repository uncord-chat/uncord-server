package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/channel"
	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/presence"
	"github.com/uncord-chat/uncord-server/internal/role"
	servercfg "github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// fakeUserRepo implements user.Repository for testing.
type fakeUserRepo struct {
	user *user.User
}

func (r *fakeUserRepo) Create(context.Context, user.CreateParams) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (r *fakeUserRepo) GetByID(_ context.Context, _ uuid.UUID) (*user.User, error) {
	if r.user == nil {
		return nil, user.ErrNotFound
	}
	return r.user, nil
}
func (r *fakeUserRepo) GetByEmail(context.Context, string) (*user.Credentials, error) {
	return nil, nil
}
func (r *fakeUserRepo) GetCredentialsByID(context.Context, uuid.UUID) (*user.Credentials, error) {
	return nil, nil
}
func (r *fakeUserRepo) VerifyEmail(context.Context, string) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (r *fakeUserRepo) ReplaceVerificationToken(context.Context, uuid.UUID, string, time.Time, time.Duration) error {
	return nil
}
func (r *fakeUserRepo) RecordLoginAttempt(context.Context, string, string, bool) error { return nil }
func (r *fakeUserRepo) UpdatePasswordHash(context.Context, uuid.UUID, string) error    { return nil }
func (r *fakeUserRepo) Update(context.Context, uuid.UUID, user.UpdateParams) (*user.User, error) {
	return nil, nil
}
func (r *fakeUserRepo) EnableMFA(context.Context, uuid.UUID, string, []string) error { return nil }
func (r *fakeUserRepo) DisableMFA(context.Context, uuid.UUID) error                  { return nil }
func (r *fakeUserRepo) GetUnusedRecoveryCodes(context.Context, uuid.UUID) ([]user.MFARecoveryCode, error) {
	return nil, nil
}
func (r *fakeUserRepo) UseRecoveryCode(context.Context, uuid.UUID) error                { return nil }
func (r *fakeUserRepo) ReplaceRecoveryCodes(context.Context, uuid.UUID, []string) error { return nil }
func (r *fakeUserRepo) DeleteWithTombstones(context.Context, uuid.UUID, []user.Tombstone) error {
	return nil
}
func (r *fakeUserRepo) CheckTombstone(context.Context, user.TombstoneType, string) (bool, error) {
	return false, nil
}

// fakeServerRepo implements servercfg.Repository for testing.
type fakeServerRepo struct {
	cfg *servercfg.Config
}

func (r *fakeServerRepo) Get(context.Context) (*servercfg.Config, error) {
	return r.cfg, nil
}
func (r *fakeServerRepo) Update(context.Context, servercfg.UpdateParams) (*servercfg.Config, error) {
	return r.cfg, nil
}

// fakeChannelRepo implements channel.Repository for testing.
type fakeChannelRepo struct {
	channels []channel.Channel
}

func (r *fakeChannelRepo) List(context.Context) ([]channel.Channel, error) {
	return r.channels, nil
}
func (r *fakeChannelRepo) GetByID(context.Context, uuid.UUID) (*channel.Channel, error) {
	return nil, nil
}
func (r *fakeChannelRepo) Create(context.Context, channel.CreateParams, int) (*channel.Channel, error) {
	return nil, nil
}
func (r *fakeChannelRepo) Update(context.Context, uuid.UUID, channel.UpdateParams) (*channel.Channel, error) {
	return nil, nil
}
func (r *fakeChannelRepo) Delete(context.Context, uuid.UUID) error { return nil }

// fakeRoleRepo implements role.Repository for testing.
type fakeRoleRepo struct {
	roles []role.Role
}

func (r *fakeRoleRepo) List(context.Context) ([]role.Role, error)              { return r.roles, nil }
func (r *fakeRoleRepo) GetByID(context.Context, uuid.UUID) (*role.Role, error) { return nil, nil }
func (r *fakeRoleRepo) Create(context.Context, role.CreateParams, int) (*role.Role, error) {
	return nil, nil
}
func (r *fakeRoleRepo) Update(context.Context, uuid.UUID, role.UpdateParams) (*role.Role, error) {
	return nil, nil
}
func (r *fakeRoleRepo) Delete(context.Context, uuid.UUID) error                 { return nil }
func (r *fakeRoleRepo) HighestPosition(context.Context, uuid.UUID) (int, error) { return 0, nil }

// fakeMemberRepo implements member.Repository for testing.
type fakeMemberRepo struct {
	members []member.MemberWithProfile
}

func (r *fakeMemberRepo) List(_ context.Context, _ *uuid.UUID, _ int) ([]member.MemberWithProfile, error) {
	return r.members, nil
}
func (r *fakeMemberRepo) GetByUserID(context.Context, uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeMemberRepo) UpdateNickname(context.Context, uuid.UUID, *string) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeMemberRepo) Delete(context.Context, uuid.UUID) error { return nil }
func (r *fakeMemberRepo) SetTimeout(context.Context, uuid.UUID, time.Time) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeMemberRepo) ClearTimeout(context.Context, uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeMemberRepo) Ban(context.Context, uuid.UUID, uuid.UUID, *string, *time.Time) error {
	return nil
}
func (r *fakeMemberRepo) Unban(context.Context, uuid.UUID) error                 { return nil }
func (r *fakeMemberRepo) ListBans(context.Context) ([]member.BanRecord, error)   { return nil, nil }
func (r *fakeMemberRepo) IsBanned(context.Context, uuid.UUID) (bool, error)      { return false, nil }
func (r *fakeMemberRepo) AssignRole(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *fakeMemberRepo) RemoveRole(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *fakeMemberRepo) CreatePending(context.Context, uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeMemberRepo) Activate(context.Context, uuid.UUID, []uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeMemberRepo) GetStatus(context.Context, uuid.UUID) (string, error) { return "", nil }
func (r *fakeMemberRepo) GetByUserIDAnyStatus(context.Context, uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}

func testConfig() *config.Config {
	return &config.Config{
		GatewayHeartbeatIntervalMS: 45000,
		GatewaySessionTTL:          5 * time.Minute,
		GatewayReplayBufferSize:    100,
		GatewayMaxConnections:      10,
		RateLimitWSCount:           120,
		RateLimitWSWindowSeconds:   60,
		JWTSecret:                  "test-secret-for-defaults-minimum-32",
		ServerURL:                  "http://localhost:8080",
	}
}

func TestAssembleReady(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)

	userID := uuid.New()
	serverID := uuid.New()
	channelID := uuid.New()
	roleID := uuid.New()

	cfg := testConfig()
	sessions := NewSessionStore(rdb, cfg.GatewaySessionTTL, cfg.GatewayReplayBufferSize)

	hub := NewHub(rdb, cfg, sessions, nil,
		&fakeUserRepo{user: &user.User{
			ID:       userID,
			Email:    "test@example.com",
			Username: "testuser",
		}},
		&fakeServerRepo{cfg: &servercfg.Config{
			ID:      serverID,
			Name:    "Test Server",
			OwnerID: userID,
		}},
		&fakeChannelRepo{channels: []channel.Channel{
			{ID: channelID, Name: "general", Type: "text"},
		}},
		&fakeRoleRepo{roles: []role.Role{
			{ID: roleID, Name: "everyone", IsEveryone: true},
		}},
		&fakeMemberRepo{members: []member.MemberWithProfile{
			{UserID: userID, Username: "testuser", Status: "active", RoleIDs: []uuid.UUID{roleID}},
		}},
		nil, nil, zerolog.Nop(),
	)

	ctx := context.Background()
	ready, err := hub.assembleReady(ctx, userID)
	if err != nil {
		t.Fatalf("assembleReady() error = %v", err)
	}

	if ready.User.ID != userID.String() {
		t.Errorf("User.ID = %q, want %q", ready.User.ID, userID.String())
	}
	if ready.Server.Name != "Test Server" {
		t.Errorf("Server.Name = %q, want %q", ready.Server.Name, "Test Server")
	}
	if len(ready.Channels) != 1 {
		t.Errorf("len(Channels) = %d, want 1", len(ready.Channels))
	}
	if len(ready.Roles) != 1 {
		t.Errorf("len(Roles) = %d, want 1", len(ready.Roles))
	}
	if len(ready.Members) != 1 {
		t.Errorf("len(Members) = %d, want 1", len(ready.Members))
	}
}

func TestHandlePubSubEventBroadcast(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	cfg := testConfig()
	sessions := NewSessionStore(rdb, cfg.GatewaySessionTTL, cfg.GatewayReplayBufferSize)

	hub := NewHub(rdb, cfg, sessions, nil, nil, nil, nil, nil, nil, nil, nil, zerolog.Nop())

	userID := uuid.New()
	client := &Client{
		hub:  hub,
		send: make(chan []byte, 256),
		log:  zerolog.Nop(),
	}
	client.mu.Lock()
	client.userID = userID
	client.sessionID = "test-session"
	client.identified = true
	client.mu.Unlock()

	hub.mu.Lock()
	hub.clients[userID] = client
	hub.mu.Unlock()

	// Simulate a non-channel-scoped event (e.g., SERVER_UPDATE).
	env := envelope{Type: string(events.ServerUpdate), Data: map[string]string{"name": "New Name"}}
	payload, _ := json.Marshal(env)

	hub.handlePubSubEvent(context.Background(), string(payload))

	select {
	case msg := <-client.send:
		var f events.Frame
		if err := json.Unmarshal(msg, &f); err != nil {
			t.Fatalf("unmarshal frame: %v", err)
		}
		if f.Op != events.OpcodeDispatch {
			t.Errorf("Op = %d, want %d", f.Op, events.OpcodeDispatch)
		}
		if f.Type == nil || *f.Type != events.ServerUpdate {
			t.Errorf("Type = %v, want %q", f.Type, events.ServerUpdate)
		}
		if f.Seq == nil || *f.Seq != 1 {
			t.Errorf("Seq = %v, want 1", f.Seq)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatched message")
	}
}

func TestRegisterDisplacesExisting(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	cfg := testConfig()
	sessions := NewSessionStore(rdb, cfg.GatewaySessionTTL, cfg.GatewayReplayBufferSize)

	hub := NewHub(rdb, cfg, sessions, nil, nil, nil, nil, nil, nil, nil, nil, zerolog.Nop())

	userID := uuid.New()

	old := &Client{hub: hub, send: make(chan []byte, 256), log: zerolog.Nop()}
	old.mu.Lock()
	old.userID = userID
	old.sessionID = "old-session"
	old.identified = true
	old.mu.Unlock()

	hub.mu.Lock()
	hub.clients[userID] = old
	hub.mu.Unlock()

	newer := &Client{hub: hub, send: make(chan []byte, 256), log: zerolog.Nop()}
	newer.mu.Lock()
	newer.userID = userID
	newer.sessionID = "new-session"
	newer.identified = true
	newer.mu.Unlock()

	if err := hub.register(newer); err != nil {
		t.Fatalf("register() error = %v", err)
	}

	// The old client's send channel should be closed (displaced).
	select {
	case _, ok := <-old.send:
		// We expect to receive an InvalidSession frame and then the channel to be closed.
		if ok {
			// Drain the frame, then check closure.
			_, ok = <-old.send
		}
		if ok {
			t.Error("old client's send channel was not closed after displacement")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for old client displacement")
	}

	hub.mu.RLock()
	current := hub.clients[userID]
	hub.mu.RUnlock()
	if current != newer {
		t.Error("registered client is not the new one")
	}
}

func TestRegisterMaxConnections(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	cfg := testConfig()
	cfg.GatewayMaxConnections = 1
	sessions := NewSessionStore(rdb, cfg.GatewaySessionTTL, cfg.GatewayReplayBufferSize)

	hub := NewHub(rdb, cfg, sessions, nil, nil, nil, nil, nil, nil, nil, nil, zerolog.Nop())

	// Register one client.
	uid1 := uuid.New()
	c1 := &Client{hub: hub, send: make(chan []byte, 256), log: zerolog.Nop()}
	c1.mu.Lock()
	c1.userID = uid1
	c1.sessionID = "s1"
	c1.identified = true
	c1.mu.Unlock()
	if err := hub.register(c1); err != nil {
		t.Fatalf("register(c1) error = %v", err)
	}

	// A second user should be rejected.
	uid2 := uuid.New()
	c2 := &Client{hub: hub, send: make(chan []byte, 256), log: zerolog.Nop()}
	c2.mu.Lock()
	c2.userID = uid2
	c2.sessionID = "s2"
	c2.identified = true
	c2.mu.Unlock()
	if err := hub.register(c2); err != ErrMaxConnections {
		t.Errorf("register(c2) error = %v, want ErrMaxConnections", err)
	}
}

func TestModelConversions(t *testing.T) {
	t.Parallel()

	t.Run("User.ToModel", func(t *testing.T) {
		t.Parallel()
		u := &user.User{
			ID:       uuid.New(),
			Email:    "user@example.com",
			Username: "alice",
		}
		m := u.ToModel()
		if m.ID != u.ID.String() {
			t.Errorf("ID = %q, want %q", m.ID, u.ID.String())
		}
		if m.Username != "alice" {
			t.Errorf("Username = %q, want %q", m.Username, "alice")
		}
	})

	t.Run("Channel.ToModel", func(t *testing.T) {
		t.Parallel()
		catID := uuid.New()
		ch := &channel.Channel{
			ID:         uuid.New(),
			CategoryID: &catID,
			Name:       "general",
			Type:       "text",
		}
		m := ch.ToModel()
		if m.Name != "general" {
			t.Errorf("Name = %q, want %q", m.Name, "general")
		}
		if m.CategoryID == nil || *m.CategoryID != catID.String() {
			t.Errorf("CategoryID = %v, want %q", m.CategoryID, catID.String())
		}
	})

	t.Run("Channel.ToModel nil category", func(t *testing.T) {
		t.Parallel()
		ch := &channel.Channel{ID: uuid.New(), Name: "no-cat"}
		m := ch.ToModel()
		if m.CategoryID != nil {
			t.Errorf("CategoryID = %v, want nil", m.CategoryID)
		}
	})

	t.Run("Role.ToModel", func(t *testing.T) {
		t.Parallel()
		r := &role.Role{
			ID:          uuid.New(),
			Name:        "admin",
			Colour:      0xFF0000,
			Position:    1,
			Hoist:       true,
			Permissions: int64(permissions.AllPermissions),
		}
		m := r.ToModel()
		if m.Name != "admin" {
			t.Errorf("Name = %q, want %q", m.Name, "admin")
		}
		if !m.Hoist {
			t.Error("Hoist = false, want true")
		}
	})

	t.Run("MemberWithProfile.ToModel with timeout", func(t *testing.T) {
		t.Parallel()
		timeout := time.Now().Add(time.Hour)
		mp := &member.MemberWithProfile{
			UserID:       uuid.New(),
			Username:     "bob",
			Status:       "active",
			TimeoutUntil: &timeout,
			RoleIDs:      []uuid.UUID{uuid.New()},
		}
		m := mp.ToModel()
		if m.TimeoutUntil == nil {
			t.Fatal("TimeoutUntil = nil, want non-nil")
		}
	})
}

func TestMemberSliceToModels(t *testing.T) {
	t.Parallel()
	ms := []member.MemberWithProfile{
		{UserID: uuid.New(), Username: "a", Status: "active"},
		{UserID: uuid.New(), Username: "b", Status: "active"},
	}
	result := memberSliceToModels(ms)
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].User.Username != "a" {
		t.Errorf("[0].User.Username = %q, want %q", result[0].User.Username, "a")
	}
}

func TestReadyDataJSON(t *testing.T) {
	t.Parallel()
	ready := models.ReadyData{
		SessionID: "test-session",
		User:      models.User{ID: "u1", Username: "alice"},
		Server:    models.ServerConfig{ID: "s1", Name: "Test"},
		Channels:  []models.Channel{{ID: "c1", Name: "general"}},
		Roles:     []models.Role{{ID: "r1", Name: "everyone"}},
		Members:   []models.Member{{User: models.MemberUser{ID: "u1", Username: "alice"}}},
		Presences: []models.PresenceState{{UserID: "u1", Status: "online"}},
	}

	data, err := json.Marshal(ready)
	if err != nil {
		t.Fatalf("marshal ReadyData: %v", err)
	}

	var decoded models.ReadyData
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal ReadyData: %v", err)
	}
	if decoded.SessionID != "test-session" {
		t.Errorf("SessionID = %q, want %q", decoded.SessionID, "test-session")
	}
	if decoded.User.Username != "alice" {
		t.Errorf("User.Username = %q, want %q", decoded.User.Username, "alice")
	}
	if len(decoded.Presences) != 1 {
		t.Fatalf("len(Presences) = %d, want 1", len(decoded.Presences))
	}
	if decoded.Presences[0].Status != "online" {
		t.Errorf("Presences[0].Status = %q, want %q", decoded.Presences[0].Status, "online")
	}
}

func TestAssembleReadyWithPresences(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)

	userID := uuid.New()
	cfg := testConfig()
	sessions := NewSessionStore(rdb, cfg.GatewaySessionTTL, cfg.GatewayReplayBufferSize)
	presenceStore := presence.NewStore(rdb)

	// Set user as online before assembling READY.
	ctx := context.Background()
	if err := presenceStore.Set(ctx, userID, "online"); err != nil {
		t.Fatalf("presence.Set() error = %v", err)
	}

	hub := NewHub(rdb, cfg, sessions, nil,
		&fakeUserRepo{user: &user.User{ID: userID, Email: "a@b.com", Username: "a"}},
		&fakeServerRepo{cfg: &servercfg.Config{ID: uuid.New(), Name: "S", OwnerID: userID}},
		&fakeChannelRepo{},
		&fakeRoleRepo{},
		&fakeMemberRepo{members: []member.MemberWithProfile{
			{UserID: userID, Username: "a", Status: "active"},
		}},
		presenceStore, nil, zerolog.Nop(),
	)

	ready, err := hub.assembleReady(ctx, userID)
	if err != nil {
		t.Fatalf("assembleReady() error = %v", err)
	}
	if len(ready.Presences) != 1 {
		t.Fatalf("len(Presences) = %d, want 1", len(ready.Presences))
	}
	if ready.Presences[0].UserID != userID.String() {
		t.Errorf("Presences[0].UserID = %q, want %q", ready.Presences[0].UserID, userID.String())
	}
}

func TestHandlePubSubEventEphemeral(t *testing.T) {
	t.Parallel()
	_, rdb := newTestRedis(t)
	cfg := testConfig()
	sessions := NewSessionStore(rdb, cfg.GatewaySessionTTL, cfg.GatewayReplayBufferSize)

	hub := NewHub(rdb, cfg, sessions, nil, nil, nil, nil, nil, nil, nil, nil, zerolog.Nop())

	userID := uuid.New()
	client := &Client{
		hub:  hub,
		send: make(chan []byte, 256),
		log:  zerolog.Nop(),
	}
	client.mu.Lock()
	client.userID = userID
	client.sessionID = "test-session"
	client.identified = true
	client.mu.Unlock()

	hub.mu.Lock()
	hub.clients[userID] = client
	hub.mu.Unlock()

	// The envelope omits channel_id to avoid triggering the permission filter (which requires a non-nil resolver).
	// Channel-scoped permission filtering is exercised separately. This test focuses on the ephemeral dispatch path
	// (no sequence number, no replay buffer).
	env := envelope{Type: string(events.TypingStart), Data: map[string]string{
		"user_id": uuid.New().String(),
	}}
	payload, _ := json.Marshal(env)

	hub.handlePubSubEvent(context.Background(), string(payload))

	select {
	case msg := <-client.send:
		var f events.Frame
		if err := json.Unmarshal(msg, &f); err != nil {
			t.Fatalf("unmarshal frame: %v", err)
		}
		if f.Op != events.OpcodeDispatch {
			t.Errorf("Op = %d, want %d", f.Op, events.OpcodeDispatch)
		}
		if f.Type == nil || *f.Type != events.TypingStart {
			t.Errorf("Type = %v, want %q", f.Type, events.TypingStart)
		}
		if f.Seq != nil {
			t.Errorf("Seq = %v, want nil (ephemeral)", f.Seq)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ephemeral dispatch")
	}

	// Verify no sequence was consumed.
	if seq := client.currentSeq(); seq != 0 {
		t.Errorf("currentSeq() = %d, want 0 (ephemeral should not increment)", seq)
	}
}
