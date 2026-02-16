package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/invite"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/onboarding"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// --- fakes ---

// fakeInviteRepo implements invite.Repository for handler tests.
type fakeInviteRepo struct {
	invites []invite.Invite
}

func newFakeInviteRepo() *fakeInviteRepo {
	return &fakeInviteRepo{}
}

func (r *fakeInviteRepo) Create(_ context.Context, creatorID uuid.UUID, params invite.CreateParams) (*invite.Invite, error) {
	// Check channel exists (simulate FK).
	if params.ChannelID == uuid.Nil {
		return nil, invite.ErrChannelNotFound
	}

	inv := invite.Invite{
		ID:            uuid.New(),
		Code:          "testcode",
		ChannelID:     params.ChannelID,
		CreatorID:     creatorID,
		MaxUses:       params.MaxUses,
		UseCount:      0,
		MaxAgeSeconds: params.MaxAgeSeconds,
		CreatedAt:     time.Now(),
	}
	r.invites = append(r.invites, inv)
	return &r.invites[len(r.invites)-1], nil
}

func (r *fakeInviteRepo) GetByCode(_ context.Context, code string) (*invite.Invite, error) {
	for i := range r.invites {
		if r.invites[i].Code == code {
			return &r.invites[i], nil
		}
	}
	return nil, invite.ErrNotFound
}

func (r *fakeInviteRepo) List(_ context.Context, after *uuid.UUID, limit int) ([]invite.Invite, error) {
	start := 0
	if after != nil {
		for i, inv := range r.invites {
			if inv.ID == *after {
				start = i + 1
				break
			}
		}
	}
	if start >= len(r.invites) {
		return nil, nil
	}
	end := start + limit
	if end > len(r.invites) {
		end = len(r.invites)
	}
	return r.invites[start:end], nil
}

func (r *fakeInviteRepo) Delete(_ context.Context, code string) error {
	for i := range r.invites {
		if r.invites[i].Code == code {
			r.invites = append(r.invites[:i], r.invites[i+1:]...)
			return nil
		}
	}
	return invite.ErrNotFound
}

func (r *fakeInviteRepo) Use(_ context.Context, code string) (*invite.Invite, error) {
	for i := range r.invites {
		if r.invites[i].Code == code {
			inv := &r.invites[i]
			if inv.ExpiresAt != nil && !inv.ExpiresAt.After(time.Now()) {
				return nil, invite.ErrExpired
			}
			if inv.MaxUses != nil && inv.UseCount >= *inv.MaxUses {
				return nil, invite.ErrMaxUsesReached
			}
			inv.UseCount++
			return inv, nil
		}
	}
	return nil, invite.ErrNotFound
}

// fakeInviteOnboardingRepo implements onboarding.Repository for invite handler tests.
type fakeInviteOnboardingRepo struct {
	cfg *onboarding.Config
}

func newFakeInviteOnboardingRepo() *fakeInviteOnboardingRepo {
	return &fakeInviteOnboardingRepo{cfg: &onboarding.Config{}}
}

func (r *fakeInviteOnboardingRepo) Get(context.Context) (*onboarding.Config, error) {
	return r.cfg, nil
}

func (r *fakeInviteOnboardingRepo) Update(context.Context, onboarding.UpdateParams) (*onboarding.Config, error) {
	return r.cfg, nil
}

// fakeInviteUserRepo implements user.Repository for invite handler tests. Only GetByID is used.
type fakeInviteUserRepo struct {
	users map[uuid.UUID]*user.User
}

func newFakeInviteUserRepo() *fakeInviteUserRepo {
	return &fakeInviteUserRepo{users: make(map[uuid.UUID]*user.User)}
}

func (r *fakeInviteUserRepo) GetByID(_ context.Context, id uuid.UUID) (*user.User, error) {
	u, ok := r.users[id]
	if !ok {
		return nil, user.ErrNotFound
	}
	return u, nil
}

func (r *fakeInviteUserRepo) Create(context.Context, user.CreateParams) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (r *fakeInviteUserRepo) GetByEmail(context.Context, string) (*user.Credentials, error) {
	return nil, nil
}
func (r *fakeInviteUserRepo) GetCredentialsByID(context.Context, uuid.UUID) (*user.Credentials, error) {
	return nil, nil
}
func (r *fakeInviteUserRepo) VerifyEmail(context.Context, string) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (r *fakeInviteUserRepo) ReplaceVerificationToken(context.Context, uuid.UUID, string, time.Time, time.Duration) error {
	return nil
}
func (r *fakeInviteUserRepo) RecordLoginAttempt(context.Context, string, string, bool) error {
	return nil
}
func (r *fakeInviteUserRepo) UpdatePasswordHash(context.Context, uuid.UUID, string) error {
	return nil
}
func (r *fakeInviteUserRepo) Update(context.Context, uuid.UUID, user.UpdateParams) (*user.User, error) {
	return nil, nil
}
func (r *fakeInviteUserRepo) EnableMFA(context.Context, uuid.UUID, string, []string) error {
	return nil
}
func (r *fakeInviteUserRepo) DisableMFA(context.Context, uuid.UUID) error { return nil }
func (r *fakeInviteUserRepo) GetUnusedRecoveryCodes(context.Context, uuid.UUID) ([]user.MFARecoveryCode, error) {
	return nil, nil
}
func (r *fakeInviteUserRepo) UseRecoveryCode(context.Context, uuid.UUID) error { return nil }
func (r *fakeInviteUserRepo) ReplaceRecoveryCodes(context.Context, uuid.UUID, []string) error {
	return nil
}
func (r *fakeInviteUserRepo) DeleteWithTombstones(context.Context, uuid.UUID, []user.Tombstone) error {
	return nil
}
func (r *fakeInviteUserRepo) CheckTombstone(context.Context, user.TombstoneType, string) (bool, error) {
	return false, nil
}

// fakeInviteMemberRepo implements member.Repository for invite handler tests.
type fakeInviteMemberRepo struct {
	members []member.MemberWithProfile
	bans    []uuid.UUID
}

func newFakeInviteMemberRepo() *fakeInviteMemberRepo {
	return &fakeInviteMemberRepo{}
}

func (r *fakeInviteMemberRepo) IsBanned(_ context.Context, userID uuid.UUID) (bool, error) {
	for _, id := range r.bans {
		if id == userID {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeInviteMemberRepo) CreatePending(_ context.Context, userID uuid.UUID) (*member.MemberWithProfile, error) {
	for _, m := range r.members {
		if m.UserID == userID {
			return nil, member.ErrAlreadyMember
		}
	}
	m := member.MemberWithProfile{
		UserID:   userID,
		Username: "joined_user",
		Status:   "pending",
		JoinedAt: time.Now(),
	}
	r.members = append(r.members, m)
	return &r.members[len(r.members)-1], nil
}

func (r *fakeInviteMemberRepo) Activate(_ context.Context, userID uuid.UUID, _ []uuid.UUID) (*member.MemberWithProfile, error) {
	for i := range r.members {
		if r.members[i].UserID == userID {
			if r.members[i].Status != "pending" {
				return nil, member.ErrNotPending
			}
			r.members[i].Status = "active"
			return &r.members[i], nil
		}
	}
	return nil, member.ErrNotPending
}

// Unused methods to satisfy the member.Repository interface.
func (r *fakeInviteMemberRepo) List(context.Context, *uuid.UUID, int) ([]member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeInviteMemberRepo) GetByUserID(context.Context, uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeInviteMemberRepo) UpdateNickname(context.Context, uuid.UUID, *string) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeInviteMemberRepo) Delete(context.Context, uuid.UUID) error { return nil }
func (r *fakeInviteMemberRepo) SetTimeout(context.Context, uuid.UUID, time.Time) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeInviteMemberRepo) ClearTimeout(context.Context, uuid.UUID) (*member.MemberWithProfile, error) {
	return nil, nil
}
func (r *fakeInviteMemberRepo) Ban(context.Context, uuid.UUID, uuid.UUID, *string, *time.Time) error {
	return nil
}
func (r *fakeInviteMemberRepo) Unban(context.Context, uuid.UUID) error                 { return nil }
func (r *fakeInviteMemberRepo) ListBans(context.Context) ([]member.BanRecord, error)   { return nil, nil }
func (r *fakeInviteMemberRepo) AssignRole(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *fakeInviteMemberRepo) RemoveRole(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (r *fakeInviteMemberRepo) GetStatus(_ context.Context, userID uuid.UUID) (string, error) {
	for i := range r.members {
		if r.members[i].UserID == userID {
			return r.members[i].Status, nil
		}
	}
	return "", member.ErrNotFound
}

func (r *fakeInviteMemberRepo) GetByUserIDAnyStatus(_ context.Context, userID uuid.UUID) (*member.MemberWithProfile, error) {
	for i := range r.members {
		if r.members[i].UserID == userID {
			return &r.members[i], nil
		}
	}
	return nil, member.ErrNotFound
}

// --- seed helpers ---

func seedInvite(repo *fakeInviteRepo, code string, channelID uuid.UUID) *invite.Invite {
	inv := invite.Invite{
		ID:        uuid.New(),
		Code:      code,
		ChannelID: channelID,
		CreatorID: uuid.New(),
		UseCount:  0,
		CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	repo.invites = append(repo.invites, inv)
	return &repo.invites[len(repo.invites)-1]
}

// --- test app factory ---

func testInviteApp(t *testing.T, inviteRepo *fakeInviteRepo, onboardingRepo *fakeInviteOnboardingRepo, memberRepo *fakeInviteMemberRepo, userRepo *fakeInviteUserRepo, callerID uuid.UUID) *fiber.App {
	t.Helper()
	handler := NewInviteHandler(inviteRepo, onboardingRepo, memberRepo, userRepo, zerolog.Nop())
	app := fiber.New()
	app.Use(fakeAuth(callerID))

	// Server invite routes.
	app.Post("/server/invites", handler.CreateInvite)
	app.Get("/server/invites", handler.ListInvites)

	// Invite action routes.
	app.Delete("/invites/:code", handler.DeleteInvite)
	app.Post("/invites/:code/join", handler.JoinViaInvite)

	return app
}

// --- CreateInvite tests ---

func TestCreateInvite_Success(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()
	repo := newFakeInviteRepo()
	app := testInviteApp(t, repo, newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/server/invites",
		`{"channel_id":"`+channelID.String()+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusCreated)
	}

	env := parseSuccess(t, body)
	var inv struct {
		Code      string `json:"code"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(env.Data, &inv); err != nil {
		t.Fatalf("unmarshal invite: %v", err)
	}
	if inv.Code == "" {
		t.Error("invite code is empty")
	}
	if inv.ChannelID != channelID.String() {
		t.Errorf("channel_id = %q, want %q", inv.ChannelID, channelID.String())
	}
}

func TestCreateInvite_InvalidBody(t *testing.T) {
	t.Parallel()
	app := testInviteApp(t, newFakeInviteRepo(), newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/server/invites", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestCreateInvite_InvalidChannelID(t *testing.T) {
	t.Parallel()
	app := testInviteApp(t, newFakeInviteRepo(), newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/server/invites",
		`{"channel_id":"not-a-uuid"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateInvite_ChannelNotFound(t *testing.T) {
	t.Parallel()
	app := testInviteApp(t, newFakeInviteRepo(), newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	// uuid.Nil triggers ErrChannelNotFound in the fake.
	resp := doReq(t, app, jsonReq(http.MethodPost, "/server/invites",
		`{"channel_id":"`+uuid.Nil.String()+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownChannel) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownChannel)
	}
}

func TestCreateInvite_NegativeMaxUses(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()
	app := testInviteApp(t, newFakeInviteRepo(), newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/server/invites",
		`{"channel_id":"`+channelID.String()+`","max_uses":-1}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateInvite_NegativeMaxAge(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()
	app := testInviteApp(t, newFakeInviteRepo(), newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/server/invites",
		`{"channel_id":"`+channelID.String()+`","max_age_seconds":-1}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

// --- ListInvites tests ---

func TestListInvites_Empty(t *testing.T) {
	t.Parallel()
	app := testInviteApp(t, newFakeInviteRepo(), newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/server/invites", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var invites []json.RawMessage
	if err := json.Unmarshal(env.Data, &invites); err != nil {
		t.Fatalf("unmarshal invites: %v", err)
	}
	if len(invites) != 0 {
		t.Errorf("got %d invites, want 0", len(invites))
	}
}

func TestListInvites_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeInviteRepo()
	seedInvite(repo, "abc123", uuid.New())
	seedInvite(repo, "def456", uuid.New())
	app := testInviteApp(t, repo, newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/server/invites", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var invites []struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(env.Data, &invites); err != nil {
		t.Fatalf("unmarshal invites: %v", err)
	}
	if len(invites) != 2 {
		t.Fatalf("got %d invites, want 2", len(invites))
	}
}

func TestListInvites_Pagination(t *testing.T) {
	t.Parallel()
	repo := newFakeInviteRepo()
	first := seedInvite(repo, "abc123", uuid.New())
	seedInvite(repo, "def456", uuid.New())
	app := testInviteApp(t, repo, newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/server/invites?after="+first.ID.String()+"&limit=1", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var invites []struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(env.Data, &invites); err != nil {
		t.Fatalf("unmarshal invites: %v", err)
	}
	if len(invites) != 1 {
		t.Fatalf("got %d invites, want 1", len(invites))
	}
	if invites[0].Code != "def456" {
		t.Errorf("code = %q, want %q", invites[0].Code, "def456")
	}
}

// --- DeleteInvite tests ---

func TestDeleteInvite_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeInviteRepo()
	seedInvite(repo, "abc123", uuid.New())
	app := testInviteApp(t, repo, newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/invites/abc123", ""))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
	if len(repo.invites) != 0 {
		t.Errorf("invites remaining = %d, want 0", len(repo.invites))
	}
}

func TestDeleteInvite_NotFound(t *testing.T) {
	t.Parallel()
	app := testInviteApp(t, newFakeInviteRepo(), newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/invites/nonexistent", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownInvite) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownInvite)
	}
}

// --- JoinViaInvite tests ---

func TestJoinViaInvite_Success(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	repo := newFakeInviteRepo()
	seedInvite(repo, "abc123", uuid.New())
	userRepo := newFakeInviteUserRepo()
	userRepo.users[callerID] = &user.User{
		ID:        callerID,
		CreatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	app := testInviteApp(t, repo, newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), userRepo, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/invites/abc123/join", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var m struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(env.Data, &m); err != nil {
		t.Fatalf("unmarshal member: %v", err)
	}
	if m.Status != "pending" {
		t.Errorf("status = %q, want %q", m.Status, "pending")
	}
}

func TestJoinViaInvite_NotFound(t *testing.T) {
	t.Parallel()
	app := testInviteApp(t, newFakeInviteRepo(), newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/invites/nonexistent/join", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownInvite) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownInvite)
	}
}

func TestJoinViaInvite_Expired(t *testing.T) {
	t.Parallel()
	repo := newFakeInviteRepo()
	inv := seedInvite(repo, "expired", uuid.New())
	past := time.Now().Add(-1 * time.Hour)
	inv.ExpiresAt = &past
	app := testInviteApp(t, repo, newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/invites/expired/join", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestJoinViaInvite_MaxUsesReached(t *testing.T) {
	t.Parallel()
	repo := newFakeInviteRepo()
	inv := seedInvite(repo, "maxed", uuid.New())
	maxUses := 1
	inv.MaxUses = &maxUses
	inv.UseCount = 1
	app := testInviteApp(t, repo, newFakeInviteOnboardingRepo(), newFakeInviteMemberRepo(), newFakeInviteUserRepo(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/invites/maxed/join", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestJoinViaInvite_AlreadyMember(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	repo := newFakeInviteRepo()
	seedInvite(repo, "abc123", uuid.New())
	memberRepo := newFakeInviteMemberRepo()
	memberRepo.members = append(memberRepo.members, member.MemberWithProfile{
		UserID:   callerID,
		Username: "existing",
		Status:   "active",
		JoinedAt: time.Now(),
	})
	app := testInviteApp(t, repo, newFakeInviteOnboardingRepo(), memberRepo, newFakeInviteUserRepo(), callerID)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/invites/abc123/join", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.AlreadyMember) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.AlreadyMember)
	}
}

func TestJoinViaInvite_Banned(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	repo := newFakeInviteRepo()
	seedInvite(repo, "abc123", uuid.New())
	memberRepo := newFakeInviteMemberRepo()
	memberRepo.bans = append(memberRepo.bans, callerID)
	app := testInviteApp(t, repo, newFakeInviteOnboardingRepo(), memberRepo, newFakeInviteUserRepo(), callerID)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/invites/abc123/join", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.Banned) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.Banned)
	}
}

func TestJoinViaInvite_AccountTooYoung(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	repo := newFakeInviteRepo()
	seedInvite(repo, "abc123", uuid.New())
	onboardingRepo := &fakeInviteOnboardingRepo{cfg: &onboarding.Config{MinAccountAgeSeconds: 86400}} // 1 day
	userRepo := newFakeInviteUserRepo()
	userRepo.users[callerID] = &user.User{
		ID:        callerID,
		CreatedAt: time.Now(), // Account just created.
	}
	app := testInviteApp(t, repo, onboardingRepo, newFakeInviteMemberRepo(), userRepo, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/invites/abc123/join", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}
