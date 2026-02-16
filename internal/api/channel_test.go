package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/channel"
	"github.com/uncord-chat/uncord-server/internal/invite"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/permission"
)

// fakeChannelRepo implements channel.Repository for handler tests.
type fakeChannelRepo struct {
	channels   []channel.Channel
	maxReached bool
	categories map[uuid.UUID]bool // tracks known categories for Create/Update
}

func newFakeChannelRepo() *fakeChannelRepo {
	return &fakeChannelRepo{categories: make(map[uuid.UUID]bool)}
}

func (r *fakeChannelRepo) List(_ context.Context) ([]channel.Channel, error) {
	return r.channels, nil
}

func (r *fakeChannelRepo) GetByID(_ context.Context, id uuid.UUID) (*channel.Channel, error) {
	for i := range r.channels {
		if r.channels[i].ID == id {
			return &r.channels[i], nil
		}
	}
	return nil, channel.ErrNotFound
}

func (r *fakeChannelRepo) Create(_ context.Context, params channel.CreateParams, _ int) (*channel.Channel, error) {
	if r.maxReached {
		return nil, channel.ErrMaxChannelsReached
	}
	if params.CategoryID != nil {
		if !r.categories[*params.CategoryID] {
			return nil, channel.ErrCategoryNotFound
		}
	}
	now := time.Now()
	ch := channel.Channel{
		ID:              uuid.New(),
		CategoryID:      params.CategoryID,
		Name:            params.Name,
		Type:            params.Type,
		Topic:           params.Topic,
		Position:        len(r.channels),
		SlowmodeSeconds: params.SlowmodeSeconds,
		NSFW:            params.NSFW,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	r.channels = append(r.channels, ch)
	return &ch, nil
}

func (r *fakeChannelRepo) Update(_ context.Context, id uuid.UUID, params channel.UpdateParams) (*channel.Channel, error) {
	for i := range r.channels {
		if r.channels[i].ID == id {
			if params.Name != nil {
				r.channels[i].Name = *params.Name
			}
			if params.SetCategoryNull {
				r.channels[i].CategoryID = nil
			} else if params.CategoryID != nil {
				if !r.categories[*params.CategoryID] {
					return nil, channel.ErrCategoryNotFound
				}
				r.channels[i].CategoryID = params.CategoryID
			}
			if params.Topic != nil {
				r.channels[i].Topic = *params.Topic
			}
			if params.Position != nil {
				r.channels[i].Position = *params.Position
			}
			if params.SlowmodeSeconds != nil {
				r.channels[i].SlowmodeSeconds = *params.SlowmodeSeconds
			}
			if params.NSFW != nil {
				r.channels[i].NSFW = *params.NSFW
			}
			return &r.channels[i], nil
		}
	}
	return nil, channel.ErrNotFound
}

func (r *fakeChannelRepo) Delete(_ context.Context, id uuid.UUID) error {
	for i := range r.channels {
		if r.channels[i].ID == id {
			r.channels = append(r.channels[:i], r.channels[i+1:]...)
			return nil
		}
	}
	return channel.ErrNotFound
}

// fakePermStore implements permission.Store for handler tests. IsOwner returns true only when the queried userID
// matches ownerID. A zero-value ownerID means nobody is the owner.
type fakePermStore struct {
	ownerID uuid.UUID
}

func (s *fakePermStore) IsOwner(_ context.Context, userID uuid.UUID) (bool, error) {
	return userID == s.ownerID, nil
}

func (s *fakePermStore) RolePermissions(_ context.Context, _ uuid.UUID) ([]permission.RolePermEntry, error) {
	return []permission.RolePermEntry{
		{RoleID: uuid.New(), Permissions: permissions.AllPermissions},
	}, nil
}

func (s *fakePermStore) ChannelInfo(_ context.Context, channelID uuid.UUID) (permission.ChannelInfo, error) {
	return permission.ChannelInfo{ID: channelID}, nil
}

func (s *fakePermStore) Overrides(_ context.Context, _ permission.TargetType, _ uuid.UUID) ([]permission.Override, error) {
	return nil, nil
}

// fakePermCache implements permission.Cache with no caching.
type fakePermCache struct{}

func (c *fakePermCache) Get(_ context.Context, _, _ uuid.UUID) (permissions.Permission, bool, error) {
	return 0, false, nil
}

func (c *fakePermCache) Set(_ context.Context, _, _ uuid.UUID, _ permissions.Permission) error {
	return nil
}

func (c *fakePermCache) GetMany(_ context.Context, _ uuid.UUID, _ []uuid.UUID) (map[uuid.UUID]permissions.Permission, error) {
	return nil, nil
}

func (c *fakePermCache) SetMany(_ context.Context, _ uuid.UUID, _ map[uuid.UUID]permissions.Permission) error {
	return nil
}

func (c *fakePermCache) DeleteByUser(_ context.Context, _ uuid.UUID) error    { return nil }
func (c *fakePermCache) DeleteByChannel(_ context.Context, _ uuid.UUID) error { return nil }
func (c *fakePermCache) DeleteExact(_ context.Context, _, _ uuid.UUID) error  { return nil }
func (c *fakePermCache) DeleteAll(_ context.Context) error                    { return nil }

// denyViewPermStore returns a Store that denies ViewChannels for all users (no roles, not owner).
type denyViewPermStore struct{}

func (s *denyViewPermStore) IsOwner(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (s *denyViewPermStore) RolePermissions(_ context.Context, _ uuid.UUID) ([]permission.RolePermEntry, error) {
	return nil, nil
}

func (s *denyViewPermStore) ChannelInfo(_ context.Context, channelID uuid.UUID) (permission.ChannelInfo, error) {
	return permission.ChannelInfo{ID: channelID}, nil
}

func (s *denyViewPermStore) Overrides(_ context.Context, _ permission.TargetType, _ uuid.UUID) ([]permission.Override, error) {
	return nil, nil
}

// fakeChannelMemberRepo implements member.Repository for channel handler tests. GetStatus returns the configured
// status for known users, or ErrNotFound.
type fakeChannelMemberRepo struct {
	statuses map[uuid.UUID]string
}

func newFakeChannelMemberRepo(userID uuid.UUID) *fakeChannelMemberRepo {
	return &fakeChannelMemberRepo{
		statuses: map[uuid.UUID]string{userID: models.MemberStatusActive},
	}
}

func (r *fakeChannelMemberRepo) GetStatus(_ context.Context, userID uuid.UUID) (string, error) {
	s, ok := r.statuses[userID]
	if !ok {
		return "", member.ErrNotFound
	}
	return s, nil
}

func (r *fakeChannelMemberRepo) GetByUserIDAnyStatus(context.Context, uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeChannelMemberRepo) List(context.Context, *uuid.UUID, int) ([]member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeChannelMemberRepo) GetByUserID(context.Context, uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeChannelMemberRepo) UpdateNickname(context.Context, uuid.UUID, *string) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeChannelMemberRepo) Delete(context.Context, uuid.UUID) error { return nil }
func (r *fakeChannelMemberRepo) SetTimeout(context.Context, uuid.UUID, time.Time) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeChannelMemberRepo) ClearTimeout(context.Context, uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeChannelMemberRepo) Ban(context.Context, uuid.UUID, uuid.UUID, *string, *time.Time) error {
	return nil
}
func (r *fakeChannelMemberRepo) Unban(context.Context, uuid.UUID) error { return nil }
func (r *fakeChannelMemberRepo) ListBans(context.Context) ([]member.BanRecord, error) {
	return nil, nil
}
func (r *fakeChannelMemberRepo) IsBanned(context.Context, uuid.UUID) (bool, error)      { return false, nil }
func (r *fakeChannelMemberRepo) AssignRole(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *fakeChannelMemberRepo) RemoveRole(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *fakeChannelMemberRepo) CreatePending(context.Context, uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeChannelMemberRepo) Activate(context.Context, uuid.UUID, []uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}

// fakeOnboardingConfig implements onboardingConfigSource for channel handler tests.
type fakeOnboardingConfig struct {
	cfg *invite.OnboardingConfig
}

func (f *fakeOnboardingConfig) GetOnboardingConfig(context.Context) (*invite.OnboardingConfig, error) {
	if f.cfg != nil {
		return f.cfg, nil
	}
	return &invite.OnboardingConfig{}, nil
}

func seedChannel(repo *fakeChannelRepo) *channel.Channel {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ch := channel.Channel{
		ID:              uuid.New(),
		Name:            "general",
		Type:            models.ChannelTypeText,
		Topic:           "",
		Position:        0,
		SlowmodeSeconds: 0,
		NSFW:            false,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	repo.channels = append(repo.channels, ch)
	return &ch
}

func testChannelApp(t *testing.T, repo channel.Repository, resolver *permission.Resolver, maxChannels int, userID uuid.UUID) *fiber.App {
	t.Helper()
	memberRepo := newFakeChannelMemberRepo(userID)
	onboarding := &fakeOnboardingConfig{}
	handler := NewChannelHandler(repo, memberRepo, onboarding, resolver, nil, maxChannels, zerolog.Nop())
	app := fiber.New()

	app.Use(fakeAuth(userID))

	app.Get("/channels", handler.ListChannels)
	app.Post("/channels", handler.CreateChannel)
	app.Get("/channels/:channelID", handler.GetChannel)
	app.Patch("/channels/:channelID", handler.UpdateChannel)
	app.Delete("/channels/:channelID", handler.DeleteChannel)
	return app
}

func allowAllResolver() *permission.Resolver {
	return permission.NewResolver(&fakePermStore{}, &fakePermCache{}, zerolog.Nop())
}

func denyAllResolver() *permission.Resolver {
	return permission.NewResolver(&denyViewPermStore{}, &fakePermCache{}, zerolog.Nop())
}

// --- List tests ---

func TestListChannels_Unauthenticated(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.Nil)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.Unauthorised) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.Unauthorised)
	}
}

func TestListChannels_Empty(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var chs []json.RawMessage
	if err := json.Unmarshal(env.Data, &chs); err != nil {
		t.Fatalf("unmarshal channels: %v", err)
	}
	if len(chs) != 0 {
		t.Errorf("got %d channels, want 0", len(chs))
	}
}

func TestListChannels_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	seedChannel(repo)
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var chs []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env.Data, &chs); err != nil {
		t.Fatalf("unmarshal channels: %v", err)
	}
	if len(chs) != 1 {
		t.Fatalf("got %d channels, want 1", len(chs))
	}
	if chs[0].Name != "general" {
		t.Errorf("name = %q, want %q", chs[0].Name, "general")
	}
	if chs[0].Type != "text" {
		t.Errorf("type = %q, want %q", chs[0].Type, "text")
	}
}

func TestListChannels_PermissionFiltering(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	seedChannel(repo)
	app := testChannelApp(t, repo, denyAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var chs []json.RawMessage
	if err := json.Unmarshal(env.Data, &chs); err != nil {
		t.Fatalf("unmarshal channels: %v", err)
	}
	if len(chs) != 0 {
		t.Errorf("got %d channels, want 0 (all should be filtered by permission)", len(chs))
	}
}

func TestListChannels_PendingMemberSeesWelcomeChannel(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	repo := newFakeChannelRepo()
	welcomeCh := seedChannel(repo)
	seedChannel(repo) // second channel the pending member should not see

	memberRepo := newFakeChannelMemberRepo(userID)
	memberRepo.statuses[userID] = models.MemberStatusPending

	onboarding := &fakeOnboardingConfig{cfg: &invite.OnboardingConfig{WelcomeChannelID: &welcomeCh.ID}}
	handler := NewChannelHandler(repo, memberRepo, onboarding, allowAllResolver(), nil, 500, zerolog.Nop())

	app := fiber.New()
	app.Use(fakeAuth(userID))
	app.Get("/channels", handler.ListChannels)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var chs []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &chs); err != nil {
		t.Fatalf("unmarshal channels: %v", err)
	}
	if len(chs) != 1 {
		t.Fatalf("got %d channels, want 1", len(chs))
	}
	if chs[0].ID != welcomeCh.ID.String() {
		t.Errorf("channel id = %q, want %q", chs[0].ID, welcomeCh.ID.String())
	}
}

func TestListChannels_PendingMemberNoWelcomeChannel(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	repo := newFakeChannelRepo()
	seedChannel(repo)

	memberRepo := newFakeChannelMemberRepo(userID)
	memberRepo.statuses[userID] = models.MemberStatusPending

	onboarding := &fakeOnboardingConfig{}
	handler := NewChannelHandler(repo, memberRepo, onboarding, allowAllResolver(), nil, 500, zerolog.Nop())

	app := fiber.New()
	app.Use(fakeAuth(userID))
	app.Get("/channels", handler.ListChannels)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var chs []json.RawMessage
	if err := json.Unmarshal(env.Data, &chs); err != nil {
		t.Fatalf("unmarshal channels: %v", err)
	}
	if len(chs) != 0 {
		t.Errorf("got %d channels, want 0", len(chs))
	}
}

func TestListChannels_NonMemberSeesEmpty(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	repo := newFakeChannelRepo()
	seedChannel(repo)

	memberRepo := &fakeChannelMemberRepo{statuses: map[uuid.UUID]string{}}
	onboarding := &fakeOnboardingConfig{}
	handler := NewChannelHandler(repo, memberRepo, onboarding, allowAllResolver(), nil, 500, zerolog.Nop())

	app := fiber.New()
	app.Use(fakeAuth(userID))
	app.Get("/channels", handler.ListChannels)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var chs []json.RawMessage
	if err := json.Unmarshal(env.Data, &chs); err != nil {
		t.Fatalf("unmarshal channels: %v", err)
	}
	if len(chs) != 0 {
		t.Errorf("got %d channels, want 0", len(chs))
	}
}

// --- Create tests ---

func TestCreateChannel_InvalidJSON(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestCreateChannel_EmptyName(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels", `{"name":""}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateChannel_InvalidType(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels", `{"name":"test","type":"video"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateChannel_TopicTooLong(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	longTopic := strings.Repeat("a", 1025)
	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels", `{"name":"test","topic":"`+longTopic+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateChannel_SlowmodeOutOfRange(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels", `{"name":"test","slowmode_seconds":99999}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateChannel_MaxReached(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	repo.maxReached = true
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels", `{"name":"test"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MaxChannelsReached) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MaxChannelsReached)
	}
}

func TestCreateChannel_CategoryNotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	catID := uuid.New().String()
	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels", `{"name":"test","category_id":"`+catID+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownCategory) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownCategory)
	}
}

func TestCreateChannel_DefaultsToText(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels", `{"name":"general"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusCreated)
	}

	env := parseSuccess(t, body)
	var ch struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env.Data, &ch); err != nil {
		t.Fatalf("unmarshal channel: %v", err)
	}
	if ch.Type != "text" {
		t.Errorf("type = %q, want %q", ch.Type, "text")
	}
}

func TestCreateChannel_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels", `{"name":"voice-chat","type":"voice","nsfw":true}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusCreated)
	}

	env := parseSuccess(t, body)
	var ch struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
		NSFW bool   `json:"nsfw"`
	}
	if err := json.Unmarshal(env.Data, &ch); err != nil {
		t.Fatalf("unmarshal channel: %v", err)
	}
	if ch.Name != "voice-chat" {
		t.Errorf("name = %q, want %q", ch.Name, "voice-chat")
	}
	if ch.Type != "voice" {
		t.Errorf("type = %q, want %q", ch.Type, "voice")
	}
	if !ch.NSFW {
		t.Error("nsfw = false, want true")
	}
	if ch.ID == "" {
		t.Error("id is empty")
	}
}

// --- Get tests ---

func TestGetChannel_InvalidID(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/not-a-uuid", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidChannelID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidChannelID)
	}
}

func TestGetChannel_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+uuid.New().String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownChannel) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownChannel)
	}
}

func TestGetChannel_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	ch := seedChannel(repo)
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+ch.ID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal channel: %v", err)
	}
	if result.ID != ch.ID.String() {
		t.Errorf("id = %q, want %q", result.ID, ch.ID.String())
	}
	if result.Name != "general" {
		t.Errorf("name = %q, want %q", result.Name, "general")
	}
}

// --- Update tests ---

func TestUpdateChannel_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/channels/"+uuid.New().String(), `{"name":"updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownChannel) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownChannel)
	}
}

func TestUpdateChannel_NameValidation(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	ch := seedChannel(repo)
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	longName := strings.Repeat("a", 101)
	resp := doReq(t, app, jsonReq(http.MethodPatch, "/channels/"+ch.ID.String(), `{"name":"`+longName+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateChannel_RemoveCategory(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	catID := uuid.New()
	repo.categories[catID] = true

	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ch := channel.Channel{
		ID:         uuid.New(),
		CategoryID: &catID,
		Name:       "test",
		Type:       models.ChannelTypeText,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	repo.channels = append(repo.channels, ch)

	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	// Empty string category_id should remove the channel from its category.
	resp := doReq(t, app, jsonReq(http.MethodPatch, "/channels/"+ch.ID.String(), `{"category_id":""}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		CategoryID *string `json:"category_id"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal channel: %v", err)
	}
	if result.CategoryID != nil {
		t.Errorf("category_id = %v, want nil", result.CategoryID)
	}
}

func TestUpdateChannel_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	ch := seedChannel(repo)
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/channels/"+ch.ID.String(), `{"name":"updated","topic":"New topic"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Name  string `json:"name"`
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal channel: %v", err)
	}
	if result.Name != "updated" {
		t.Errorf("name = %q, want %q", result.Name, "updated")
	}
	if result.Topic != "New topic" {
		t.Errorf("topic = %q, want %q", result.Topic, "New topic")
	}
}

func TestUpdateChannel_EmptyBody(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	ch := seedChannel(repo)
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/channels/"+ch.ID.String(), `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal channel: %v", err)
	}
	if result.Name != "general" {
		t.Errorf("name = %q, want %q (should be unchanged)", result.Name, "general")
	}
}

// --- Delete tests ---

func TestDeleteChannel_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/channels/"+uuid.New().String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownChannel) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownChannel)
	}
}

func TestDeleteChannel_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeChannelRepo()
	ch := seedChannel(repo)
	app := testChannelApp(t, repo, allowAllResolver(), 500, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/channels/"+ch.ID.String(), ""))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
	if len(repo.channels) != 0 {
		t.Errorf("channels remaining = %d, want 0", len(repo.channels))
	}
}
