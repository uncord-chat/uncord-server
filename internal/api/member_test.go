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

	"github.com/uncord-chat/uncord-server/internal/member"
)

// --- fakes ---

// fakeMemberRepo implements member.Repository for handler tests.
type fakeMemberRepo struct {
	members []member.MemberWithProfile
	bans    []member.BanRecord
	roles   map[uuid.UUID][]uuid.UUID // userID -> roleIDs
}

func newFakeMemberRepo() *fakeMemberRepo {
	return &fakeMemberRepo{
		roles: make(map[uuid.UUID][]uuid.UUID),
	}
}

func (r *fakeMemberRepo) List(_ context.Context, after *uuid.UUID, limit int) ([]member.MemberWithProfile, error) {
	start := 0
	if after != nil {
		for i, m := range r.members {
			if m.UserID == *after {
				start = i + 1
				break
			}
		}
	}
	if start >= len(r.members) {
		return nil, nil
	}
	end := start + limit
	if end > len(r.members) {
		end = len(r.members)
	}
	return r.members[start:end], nil
}

func (r *fakeMemberRepo) GetByUserID(_ context.Context, userID uuid.UUID) (*member.MemberWithProfile, error) {
	for i := range r.members {
		if r.members[i].UserID == userID {
			return &r.members[i], nil
		}
	}
	return nil, member.ErrNotFound
}

func (r *fakeMemberRepo) UpdateNickname(_ context.Context, userID uuid.UUID, nickname *string) (*member.MemberWithProfile, error) {
	for i := range r.members {
		if r.members[i].UserID == userID {
			r.members[i].Nickname = nickname
			return &r.members[i], nil
		}
	}
	return nil, member.ErrNotFound
}

func (r *fakeMemberRepo) Delete(_ context.Context, userID uuid.UUID) error {
	for i := range r.members {
		if r.members[i].UserID == userID {
			r.members = append(r.members[:i], r.members[i+1:]...)
			return nil
		}
	}
	return member.ErrNotFound
}

func (r *fakeMemberRepo) SetTimeout(_ context.Context, userID uuid.UUID, until time.Time) (*member.MemberWithProfile, error) {
	for i := range r.members {
		if r.members[i].UserID == userID {
			r.members[i].Status = "timed_out"
			r.members[i].TimeoutUntil = &until
			return &r.members[i], nil
		}
	}
	return nil, member.ErrNotFound
}

func (r *fakeMemberRepo) ClearTimeout(_ context.Context, userID uuid.UUID) (*member.MemberWithProfile, error) {
	for i := range r.members {
		if r.members[i].UserID == userID {
			r.members[i].Status = "active"
			r.members[i].TimeoutUntil = nil
			return &r.members[i], nil
		}
	}
	return nil, member.ErrNotFound
}

func (r *fakeMemberRepo) Ban(_ context.Context, userID, _ uuid.UUID, _ *string, _ *time.Time) error {
	for _, b := range r.bans {
		if b.UserID == userID {
			return member.ErrAlreadyBanned
		}
	}
	r.bans = append(r.bans, member.BanRecord{
		UserID:    userID,
		Username:  "banned",
		CreatedAt: time.Now(),
	})
	// Remove the member.
	for i := range r.members {
		if r.members[i].UserID == userID {
			r.members = append(r.members[:i], r.members[i+1:]...)
			break
		}
	}
	return nil
}

func (r *fakeMemberRepo) Unban(_ context.Context, userID uuid.UUID) error {
	for i := range r.bans {
		if r.bans[i].UserID == userID {
			r.bans = append(r.bans[:i], r.bans[i+1:]...)
			return nil
		}
	}
	return member.ErrBanNotFound
}

func (r *fakeMemberRepo) ListBans(_ context.Context) ([]member.BanRecord, error) {
	return r.bans, nil
}

func (r *fakeMemberRepo) IsBanned(_ context.Context, userID uuid.UUID) (bool, error) {
	for _, b := range r.bans {
		if b.UserID == userID {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeMemberRepo) AssignRole(_ context.Context, userID, roleID uuid.UUID) error {
	for _, id := range r.roles[userID] {
		if id == roleID {
			return member.ErrAlreadyMember
		}
	}
	r.roles[userID] = append(r.roles[userID], roleID)
	// Update the member's RoleIDs so the re-fetched profile reflects the change.
	for i := range r.members {
		if r.members[i].UserID == userID {
			r.members[i].RoleIDs = r.roles[userID]
			break
		}
	}
	return nil
}

func (r *fakeMemberRepo) RemoveRole(_ context.Context, userID, roleID uuid.UUID) error {
	ids := r.roles[userID]
	for i, id := range ids {
		if id == roleID {
			r.roles[userID] = append(ids[:i], ids[i+1:]...)
			for j := range r.members {
				if r.members[j].UserID == userID {
					r.members[j].RoleIDs = r.roles[userID]
					break
				}
			}
			return nil
		}
	}
	return member.ErrNotFound
}

// --- seed helpers ---

func seedMember(repo *fakeMemberRepo, userID uuid.UUID, username string) *member.MemberWithProfile {
	m := member.MemberWithProfile{
		UserID:   userID,
		Username: username,
		Status:   "active",
		JoinedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	repo.members = append(repo.members, m)
	return &repo.members[len(repo.members)-1]
}

func seedBan(repo *fakeMemberRepo, userID uuid.UUID, username string) {
	repo.bans = append(repo.bans, member.BanRecord{
		UserID:    userID,
		Username:  username,
		CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	})
}

// --- test app factory ---

func testMemberApp(t *testing.T, memberRepo *fakeMemberRepo, roleRepo *fakeRoleRepo, permStore *fakePermStore, callerID uuid.UUID) *fiber.App {
	t.Helper()
	handler := NewMemberHandler(memberRepo, roleRepo, permStore, nil, zerolog.Nop())
	app := fiber.New()
	app.Use(fakeAuth(callerID))

	// Member routes: literal /@me before parameterised /:userID.
	app.Get("/members", handler.ListMembers)
	app.Get("/members/@me", handler.GetSelf)
	app.Patch("/members/@me", handler.UpdateSelf)
	app.Delete("/members/@me", handler.Leave)
	app.Get("/members/:userID", handler.GetMember)
	app.Patch("/members/:userID", handler.UpdateMember)
	app.Delete("/members/:userID", handler.KickMember)
	app.Put("/members/:userID/timeout", handler.SetTimeout)
	app.Delete("/members/:userID/timeout", handler.ClearTimeout)
	app.Put("/members/:userID/roles/:roleID", handler.AssignRole)
	app.Delete("/members/:userID/roles/:roleID", handler.RemoveRole)

	// Ban routes.
	app.Get("/bans", handler.ListBans)
	app.Put("/bans/:userID", handler.BanMember)
	app.Delete("/bans/:userID", handler.UnbanMember)

	return app
}

// --- ListMembers tests ---

func TestListMembers_Empty(t *testing.T) {
	t.Parallel()
	repo := newFakeMemberRepo()
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/members", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var members []json.RawMessage
	if err := json.Unmarshal(env.Data, &members); err != nil {
		t.Fatalf("unmarshal members: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("got %d members, want 0", len(members))
	}
}

func TestListMembers_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeMemberRepo()
	seedMember(repo, uuid.New(), "alice")
	seedMember(repo, uuid.New(), "bob")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/members", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var members []struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &members); err != nil {
		t.Fatalf("unmarshal members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("got %d members, want 2", len(members))
	}
	if members[0].User.Username != "alice" {
		t.Errorf("first member username = %q, want %q", members[0].User.Username, "alice")
	}
}

func TestListMembers_Pagination(t *testing.T) {
	t.Parallel()
	repo := newFakeMemberRepo()
	first := seedMember(repo, uuid.New(), "alice")
	seedMember(repo, uuid.New(), "bob")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/members?after="+first.UserID.String()+"&limit=1", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var members []struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &members); err != nil {
		t.Fatalf("unmarshal members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("got %d members, want 1", len(members))
	}
	if members[0].User.Username != "bob" {
		t.Errorf("member username = %q, want %q", members[0].User.Username, "bob")
	}
}

func TestListMembers_InvalidAfter(t *testing.T) {
	t.Parallel()
	repo := newFakeMemberRepo()
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/members?after=not-a-uuid", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

// --- GetSelf / GetMember tests ---

func TestGetSelf_Success(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, callerID, "alice")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/members/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var m struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &m); err != nil {
		t.Fatalf("unmarshal member: %v", err)
	}
	if m.User.Username != "alice" {
		t.Errorf("username = %q, want %q", m.User.Username, "alice")
	}
}

func TestGetSelf_NotFound(t *testing.T) {
	t.Parallel()
	app := testMemberApp(t, newFakeMemberRepo(), newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/members/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownMember) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownMember)
	}
}

func TestGetMember_Success(t *testing.T) {
	t.Parallel()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, targetID, "bob")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/members/"+targetID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var m struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &m); err != nil {
		t.Fatalf("unmarshal member: %v", err)
	}
	if m.User.Username != "bob" {
		t.Errorf("username = %q, want %q", m.User.Username, "bob")
	}
}

func TestGetMember_NotFound(t *testing.T) {
	t.Parallel()
	app := testMemberApp(t, newFakeMemberRepo(), newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/members/"+uuid.New().String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownMember) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownMember)
	}
}

// --- UpdateSelf tests ---

func TestUpdateSelf_Success(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, callerID, "alice")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/members/@me", `{"nickname":"Ali"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var m struct {
		Nickname *string `json:"nickname"`
	}
	if err := json.Unmarshal(env.Data, &m); err != nil {
		t.Fatalf("unmarshal member: %v", err)
	}
	if m.Nickname == nil || *m.Nickname != "Ali" {
		t.Errorf("nickname = %v, want %q", m.Nickname, "Ali")
	}
}

func TestUpdateSelf_ClearNickname(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	repo := newFakeMemberRepo()
	nick := "Ali"
	m := seedMember(repo, callerID, "alice")
	m.Nickname = &nick
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, callerID)

	// Sending nickname as null clears it.
	resp := doReq(t, app, jsonReq(http.MethodPatch, "/members/@me", `{"nickname":null}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Nickname *string `json:"nickname"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal member: %v", err)
	}
	if result.Nickname != nil {
		t.Errorf("nickname = %q, want nil", *result.Nickname)
	}
}

func TestUpdateSelf_NicknameTooLong(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, callerID, "alice")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, callerID)

	long := strings.Repeat("a", 33)
	resp := doReq(t, app, jsonReq(http.MethodPatch, "/members/@me", `{"nickname":"`+long+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateSelf_InvalidJSON(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, callerID, "alice")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/members/@me", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

// --- UpdateMember tests ---

func TestUpdateMember_Success(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	roleRepo.positions = map[uuid.UUID]int{callerID: 1, targetID: 5}
	app := testMemberApp(t, repo, roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/members/"+targetID.String(), `{"nickname":"Bobby"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var m struct {
		Nickname *string `json:"nickname"`
	}
	if err := json.Unmarshal(env.Data, &m); err != nil {
		t.Fatalf("unmarshal member: %v", err)
	}
	if m.Nickname == nil || *m.Nickname != "Bobby" {
		t.Errorf("nickname = %v, want %q", m.Nickname, "Bobby")
	}
}

func TestUpdateMember_Hierarchy(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	// Caller and target at same position: hierarchy blocks the action.
	roleRepo.positions = map[uuid.UUID]int{callerID: 5, targetID: 5}
	app := testMemberApp(t, repo, roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/members/"+targetID.String(), `{"nickname":"Bobby"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.RoleHierarchy) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.RoleHierarchy)
	}
}

// --- Leave tests ---

func TestLeave_Success(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, callerID, "alice")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/members/@me", ""))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
	if len(repo.members) != 0 {
		t.Errorf("members remaining = %d, want 0", len(repo.members))
	}
}

func TestLeave_OwnerProhibited(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, callerID, "alice")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{ownerID: callerID}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/members/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ServerOwner) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ServerOwner)
	}
}

// --- KickMember tests ---

func TestKickMember_Success(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	roleRepo.positions = map[uuid.UUID]int{callerID: 1, targetID: 5}
	app := testMemberApp(t, repo, roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/members/"+targetID.String(), ""))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
	if len(repo.members) != 0 {
		t.Errorf("members remaining = %d, want 0", len(repo.members))
	}
}

func TestKickMember_OwnerProtected(t *testing.T) {
	t.Parallel()
	ownerID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, ownerID, "owner")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{ownerID: ownerID}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/members/"+ownerID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ServerOwner) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ServerOwner)
	}
}

func TestKickMember_Hierarchy(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	roleRepo.positions = map[uuid.UUID]int{callerID: 5, targetID: 5}
	app := testMemberApp(t, repo, roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/members/"+targetID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.RoleHierarchy) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.RoleHierarchy)
	}
}

func TestKickMember_NotFound(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	roleRepo := newFakeRoleRepo()
	roleRepo.positions = map[uuid.UUID]int{callerID: 1, targetID: 5}
	app := testMemberApp(t, newFakeMemberRepo(), roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/members/"+targetID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownMember) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownMember)
	}
}

// --- SetTimeout tests ---

func TestSetTimeout_Success(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	roleRepo.positions = map[uuid.UUID]int{callerID: 1, targetID: 5}
	app := testMemberApp(t, repo, roleRepo, &fakePermStore{}, callerID)

	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	resp := doReq(t, app, jsonReq(http.MethodPut, "/members/"+targetID.String()+"/timeout", `{"until":"`+future+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var m struct {
		Status       string  `json:"status"`
		TimeoutUntil *string `json:"timeout_until"`
	}
	if err := json.Unmarshal(env.Data, &m); err != nil {
		t.Fatalf("unmarshal member: %v", err)
	}
	if m.Status != "timed_out" {
		t.Errorf("status = %q, want %q", m.Status, "timed_out")
	}
	if m.TimeoutUntil == nil {
		t.Error("timeout_until is nil, want non-nil")
	}
}

func TestSetTimeout_PastRejected(t *testing.T) {
	t.Parallel()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, targetID, "bob")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	resp := doReq(t, app, jsonReq(http.MethodPut, "/members/"+targetID.String()+"/timeout", `{"until":"`+past+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestSetTimeout_OwnerProtected(t *testing.T) {
	t.Parallel()
	ownerID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, ownerID, "owner")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{ownerID: ownerID}, uuid.New())

	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	resp := doReq(t, app, jsonReq(http.MethodPut, "/members/"+ownerID.String()+"/timeout", `{"until":"`+future+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ServerOwner) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ServerOwner)
	}
}

func TestSetTimeout_Hierarchy(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	roleRepo.positions = map[uuid.UUID]int{callerID: 5, targetID: 5}
	app := testMemberApp(t, repo, roleRepo, &fakePermStore{}, callerID)

	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	resp := doReq(t, app, jsonReq(http.MethodPut, "/members/"+targetID.String()+"/timeout", `{"until":"`+future+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.RoleHierarchy) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.RoleHierarchy)
	}
}

// --- ClearTimeout tests ---

func TestClearTimeout_Success(t *testing.T) {
	t.Parallel()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	m := seedMember(repo, targetID, "bob")
	timeout := time.Now().Add(24 * time.Hour)
	m.Status = "timed_out"
	m.TimeoutUntil = &timeout
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/members/"+targetID.String()+"/timeout", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Status       string  `json:"status"`
		TimeoutUntil *string `json:"timeout_until"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal member: %v", err)
	}
	if result.Status != "active" {
		t.Errorf("status = %q, want %q", result.Status, "active")
	}
	if result.TimeoutUntil != nil {
		t.Errorf("timeout_until = %q, want nil", *result.TimeoutUntil)
	}
}

// --- BanMember tests ---

func TestBanMember_Success(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	roleRepo.positions = map[uuid.UUID]int{callerID: 1, targetID: 5}
	app := testMemberApp(t, repo, roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPut, "/bans/"+targetID.String(), `{"reason":"spam"}`))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
	if len(repo.bans) != 1 {
		t.Fatalf("bans = %d, want 1", len(repo.bans))
	}
	if len(repo.members) != 0 {
		t.Errorf("members remaining = %d, want 0 (member should be removed on ban)", len(repo.members))
	}
}

func TestBanMember_OwnerProtected(t *testing.T) {
	t.Parallel()
	ownerID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, ownerID, "owner")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{ownerID: ownerID}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/bans/"+ownerID.String(), `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ServerOwner) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ServerOwner)
	}
}

func TestBanMember_AlreadyBanned(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedBan(repo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	roleRepo.positions = map[uuid.UUID]int{callerID: 1, targetID: 5}
	app := testMemberApp(t, repo, roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPut, "/bans/"+targetID.String(), `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.AlreadyExists) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.AlreadyExists)
	}
}

func TestBanMember_Hierarchy(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedMember(repo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	roleRepo.positions = map[uuid.UUID]int{callerID: 5, targetID: 5}
	app := testMemberApp(t, repo, roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPut, "/bans/"+targetID.String(), `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.RoleHierarchy) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.RoleHierarchy)
	}
}

// --- UnbanMember tests ---

func TestUnbanMember_Success(t *testing.T) {
	t.Parallel()
	targetID := uuid.New()
	repo := newFakeMemberRepo()
	seedBan(repo, targetID, "bob")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/bans/"+targetID.String(), ""))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
	if len(repo.bans) != 0 {
		t.Errorf("bans remaining = %d, want 0", len(repo.bans))
	}
}

func TestUnbanMember_NotFound(t *testing.T) {
	t.Parallel()
	app := testMemberApp(t, newFakeMemberRepo(), newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/bans/"+uuid.New().String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownBan) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownBan)
	}
}

// --- ListBans tests ---

func TestListBans_Empty(t *testing.T) {
	t.Parallel()
	app := testMemberApp(t, newFakeMemberRepo(), newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/bans", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var bans []json.RawMessage
	if err := json.Unmarshal(env.Data, &bans); err != nil {
		t.Fatalf("unmarshal bans: %v", err)
	}
	if len(bans) != 0 {
		t.Errorf("got %d bans, want 0", len(bans))
	}
}

func TestListBans_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeMemberRepo()
	seedBan(repo, uuid.New(), "banned_user")
	app := testMemberApp(t, repo, newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/bans", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var bans []struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &bans); err != nil {
		t.Fatalf("unmarshal bans: %v", err)
	}
	if len(bans) != 1 {
		t.Fatalf("got %d bans, want 1", len(bans))
	}
	if bans[0].User.Username != "banned_user" {
		t.Errorf("username = %q, want %q", bans[0].User.Username, "banned_user")
	}
}

// --- AssignRole tests ---

func TestAssignRole_Success(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	memberRepo := newFakeMemberRepo()
	seedMember(memberRepo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	r := seedRole(roleRepo)
	app := testMemberApp(t, memberRepo, roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPut, "/members/"+targetID.String()+"/roles/"+r.ID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var m struct {
		Roles []string `json:"roles"`
	}
	if err := json.Unmarshal(env.Data, &m); err != nil {
		t.Fatalf("unmarshal member: %v", err)
	}
	if len(m.Roles) != 1 {
		t.Fatalf("got %d roles, want 1", len(m.Roles))
	}
	if m.Roles[0] != r.ID.String() {
		t.Errorf("role = %q, want %q", m.Roles[0], r.ID.String())
	}
}

func TestAssignRole_EveryoneBlocked(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	memberRepo := newFakeMemberRepo()
	seedMember(memberRepo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	r := seedEveryoneRole(roleRepo)
	app := testMemberApp(t, memberRepo, roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPut, "/members/"+targetID.String()+"/roles/"+r.ID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestAssignRole_Hierarchy(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	memberRepo := newFakeMemberRepo()
	seedMember(memberRepo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	r := seedRole(roleRepo)
	// Caller at same position as role: assignment blocked.
	roleRepo.callerPos = r.Position
	app := testMemberApp(t, memberRepo, roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodPut, "/members/"+targetID.String()+"/roles/"+r.ID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.RoleHierarchy) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.RoleHierarchy)
	}
}

func TestAssignRole_RoleNotFound(t *testing.T) {
	t.Parallel()
	targetID := uuid.New()
	memberRepo := newFakeMemberRepo()
	seedMember(memberRepo, targetID, "bob")
	app := testMemberApp(t, memberRepo, newFakeRoleRepo(), &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/members/"+targetID.String()+"/roles/"+uuid.New().String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownRole) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownRole)
	}
}

func TestAssignRole_MemberNotFound(t *testing.T) {
	t.Parallel()
	roleRepo := newFakeRoleRepo()
	r := seedRole(roleRepo)
	app := testMemberApp(t, newFakeMemberRepo(), roleRepo, &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/members/"+uuid.New().String()+"/roles/"+r.ID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownMember) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownMember)
	}
}

// --- RemoveRole tests ---

func TestRemoveRole_Success(t *testing.T) {
	t.Parallel()
	callerID := uuid.New()
	targetID := uuid.New()
	memberRepo := newFakeMemberRepo()
	seedMember(memberRepo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	r := seedRole(roleRepo)
	// Pre-assign the role.
	memberRepo.roles[targetID] = []uuid.UUID{r.ID}
	memberRepo.members[0].RoleIDs = memberRepo.roles[targetID]
	app := testMemberApp(t, memberRepo, roleRepo, &fakePermStore{}, callerID)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/members/"+targetID.String()+"/roles/"+r.ID.String(), ""))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
}

func TestRemoveRole_NotAssigned(t *testing.T) {
	t.Parallel()
	targetID := uuid.New()
	memberRepo := newFakeMemberRepo()
	seedMember(memberRepo, targetID, "bob")
	roleRepo := newFakeRoleRepo()
	r := seedRole(roleRepo)
	app := testMemberApp(t, memberRepo, roleRepo, &fakePermStore{}, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/members/"+targetID.String()+"/roles/"+r.ID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownMember) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownMember)
	}
}
