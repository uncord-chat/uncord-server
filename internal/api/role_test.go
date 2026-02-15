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

	"github.com/uncord-chat/uncord-server/internal/role"
)

// fakeRoleRepo implements role.Repository for handler tests.
type fakeRoleRepo struct {
	roles      []role.Role
	maxReached bool
	// callerPos controls the default value returned by HighestPosition. If positions contains an entry for the
	// queried userID, that entry takes precedence.
	callerPos int
	// positions overrides HighestPosition for specific user IDs. Used by member handler tests that compare two
	// different users' hierarchy ranks.
	positions map[uuid.UUID]int
}

func newFakeRoleRepo() *fakeRoleRepo {
	// Default callerPos to -1 so that the hierarchy check (target.Position <= callerPos) passes for all non-negative
	// positions. Tests that exercise hierarchy enforcement set callerPos explicitly.
	return &fakeRoleRepo{callerPos: -1}
}

func (r *fakeRoleRepo) List(_ context.Context) ([]role.Role, error) {
	return r.roles, nil
}

func (r *fakeRoleRepo) GetByID(_ context.Context, id uuid.UUID) (*role.Role, error) {
	for i := range r.roles {
		if r.roles[i].ID == id {
			return &r.roles[i], nil
		}
	}
	return nil, role.ErrNotFound
}

func (r *fakeRoleRepo) Create(_ context.Context, params role.CreateParams, _ int) (*role.Role, error) {
	if r.maxReached {
		return nil, role.ErrMaxRolesReached
	}
	now := time.Now()
	created := role.Role{
		ID:          uuid.New(),
		Name:        params.Name,
		Colour:      params.Colour,
		Position:    len(r.roles),
		Hoist:       params.Hoist,
		Permissions: params.Permissions,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	r.roles = append(r.roles, created)
	return &created, nil
}

func (r *fakeRoleRepo) Update(_ context.Context, id uuid.UUID, params role.UpdateParams) (*role.Role, error) {
	for i := range r.roles {
		if r.roles[i].ID == id {
			if params.Name != nil {
				r.roles[i].Name = *params.Name
			}
			if params.Colour != nil {
				r.roles[i].Colour = *params.Colour
			}
			if params.Position != nil {
				r.roles[i].Position = *params.Position
			}
			if params.Permissions != nil {
				r.roles[i].Permissions = *params.Permissions
			}
			if params.Hoist != nil {
				r.roles[i].Hoist = *params.Hoist
			}
			return &r.roles[i], nil
		}
	}
	return nil, role.ErrNotFound
}

func (r *fakeRoleRepo) Delete(_ context.Context, id uuid.UUID) error {
	for i := range r.roles {
		if r.roles[i].ID == id {
			if r.roles[i].IsEveryone {
				return role.ErrEveryoneImmutable
			}
			r.roles = append(r.roles[:i], r.roles[i+1:]...)
			return nil
		}
	}
	return role.ErrNotFound
}

func (r *fakeRoleRepo) HighestPosition(_ context.Context, userID uuid.UUID) (int, error) {
	if r.positions != nil {
		if pos, ok := r.positions[userID]; ok {
			return pos, nil
		}
	}
	return r.callerPos, nil
}

func seedRole(repo *fakeRoleRepo) *role.Role {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	r := role.Role{
		ID:        uuid.New(),
		Name:      "Moderator",
		Colour:    3447003,
		Position:  5,
		Hoist:     true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	repo.roles = append(repo.roles, r)
	return &r
}

func seedEveryoneRole(repo *fakeRoleRepo) *role.Role {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	r := role.Role{
		ID:         uuid.New(),
		Name:       "@everyone",
		Position:   0,
		IsEveryone: true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	repo.roles = append(repo.roles, r)
	return &r
}

func testRoleApp(t *testing.T, repo role.Repository, maxRoles int, userID uuid.UUID) *fiber.App {
	t.Helper()
	handler := NewRoleHandler(repo, nil, maxRoles, zerolog.Nop())
	app := fiber.New()

	app.Use(fakeAuth(userID))

	app.Get("/roles", handler.ListRoles)
	app.Post("/roles", handler.CreateRole)
	app.Patch("/roles/:roleID", handler.UpdateRole)
	app.Delete("/roles/:roleID", handler.DeleteRole)
	return app
}

// --- List tests ---

func TestListRoles_Empty(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/roles", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var roles []json.RawMessage
	if err := json.Unmarshal(env.Data, &roles); err != nil {
		t.Fatalf("unmarshal roles: %v", err)
	}
	if len(roles) != 0 {
		t.Errorf("got %d roles, want 0", len(roles))
	}
}

func TestListRoles_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	seedRole(repo)
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/roles", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var roles []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &roles); err != nil {
		t.Fatalf("unmarshal roles: %v", err)
	}
	if len(roles) != 1 {
		t.Fatalf("got %d roles, want 1", len(roles))
	}
	if roles[0].Name != "Moderator" {
		t.Errorf("name = %q, want %q", roles[0].Name, "Moderator")
	}
}

// --- Create tests ---

func TestCreateRole_InvalidJSON(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/roles", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestCreateRole_EmptyName(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/roles", `{"name":""}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateRole_NameTooLong(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	app := testRoleApp(t, repo, 250, uuid.New())

	longName := strings.Repeat("a", 101)
	resp := doReq(t, app, jsonReq(http.MethodPost, "/roles", `{"name":"`+longName+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateRole_MaxReached(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	repo.maxReached = true
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/roles", `{"name":"New Role"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MaxRolesReached) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MaxRolesReached)
	}
}

func TestCreateRole_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/roles", `{"name":"Admin","colour":3447003,"hoist":true}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusCreated)
	}

	env := parseSuccess(t, body)
	var r struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Colour int    `json:"colour"`
		Hoist  bool   `json:"hoist"`
	}
	if err := json.Unmarshal(env.Data, &r); err != nil {
		t.Fatalf("unmarshal role: %v", err)
	}
	if r.Name != "Admin" {
		t.Errorf("name = %q, want %q", r.Name, "Admin")
	}
	if r.Colour != 3447003 {
		t.Errorf("colour = %d, want %d", r.Colour, 3447003)
	}
	if !r.Hoist {
		t.Error("hoist = false, want true")
	}
	if r.ID == "" {
		t.Error("id is empty")
	}
}

func TestCreateRole_InvalidColour(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/roles", `{"name":"Bad","colour":16777216}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateRole_InvalidPermissions(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/roles", `{"name":"Bad","permissions":-1}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

// --- Update tests ---

func TestUpdateRole_InvalidID(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/roles/not-a-uuid", `{"name":"Updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateRole_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/roles/"+uuid.New().String(), `{"name":"Updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownRole) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownRole)
	}
}

func TestUpdateRole_NameValidation(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	r := seedRole(repo)
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/roles/"+r.ID.String(), `{"name":"   "}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateRole_NegativePosition(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	r := seedRole(repo)
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/roles/"+r.ID.String(), `{"position":-1}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateRole_InvalidPermissions(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	r := seedRole(repo)
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/roles/"+r.ID.String(), `{"permissions":-1}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateRole_EveryoneRename(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	r := seedEveryoneRole(repo)
	// Caller at position 1 (below @everyone at position 0), but @everyone rename is blocked regardless of hierarchy.
	// Since callerPos defaults to math.MaxInt, the hierarchy check passes; the rename block should still trigger.
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/roles/"+r.ID.String(), `{"name":"renamed"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateRole_RoleHierarchy(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	r := seedRole(repo)
	// Give the caller a position equal to the target role (position 5). The handler should reject.
	repo.callerPos = 5
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/roles/"+r.ID.String(), `{"name":"Updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.RoleHierarchy) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.RoleHierarchy)
	}
}

func TestUpdateRole_RoleHierarchy_PositionMove(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	r := seedRole(repo)
	// Caller at position 3 (above position 5 target), but trying to move target to position 2 (above caller).
	repo.callerPos = 3
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/roles/"+r.ID.String(), `{"position":2}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.RoleHierarchy) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.RoleHierarchy)
	}
}

func TestUpdateRole_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	r := seedRole(repo)
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/roles/"+r.ID.String(), `{"name":"Senior Mod"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal role: %v", err)
	}
	if result.Name != "Senior Mod" {
		t.Errorf("name = %q, want %q", result.Name, "Senior Mod")
	}
}

func TestUpdateRole_EmptyBody(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	r := seedRole(repo)
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/roles/"+r.ID.String(), `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal role: %v", err)
	}
	if result.Name != "Moderator" {
		t.Errorf("name = %q, want %q (should be unchanged)", result.Name, "Moderator")
	}
}

// --- Delete tests ---

func TestDeleteRole_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/roles/"+uuid.New().String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownRole) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownRole)
	}
}

func TestDeleteRole_EveryoneImmutable(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	r := seedEveryoneRole(repo)
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/roles/"+r.ID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestDeleteRole_RoleHierarchy(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	r := seedRole(repo)
	// Caller at same position as target role
	repo.callerPos = 5
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/roles/"+r.ID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.RoleHierarchy) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.RoleHierarchy)
	}
}

func TestDeleteRole_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRoleRepo()
	r := seedRole(repo)
	app := testRoleApp(t, repo, 250, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/roles/"+r.ID.String(), ""))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
	if len(repo.roles) != 0 {
		t.Errorf("roles remaining = %d, want 0", len(repo.roles))
	}
}
