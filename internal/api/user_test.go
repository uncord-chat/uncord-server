package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/user"
)

func seedUser(repo *fakeRepo) *user.User {
	id := uuid.New()
	displayName := "Alice"
	c := &user.Credentials{
		User: user.User{
			ID:          id,
			Email:       "alice@example.com",
			Username:    "alice",
			DisplayName: &displayName,
		},
	}
	repo.users[c.Email] = c
	return &c.User
}

func testUserApp(t *testing.T, repo *fakeRepo, userID uuid.UUID) *fiber.App {
	t.Helper()
	handler := NewUserHandler(repo)
	app := fiber.New()

	// Inject userID into Locals to simulate RequireAuth middleware.
	app.Use(func(c fiber.Ctx) error {
		if userID != uuid.Nil {
			c.Locals("userID", userID)
		}
		return c.Next()
	})

	app.Get("/@me", handler.GetMe)
	app.Patch("/@me", handler.UpdateMe)
	return app
}

// --- GetMe tests ---

func TestGetMe_Unauthenticated(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	app := testUserApp(t, repo, uuid.Nil)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.Unauthorised) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.Unauthorised)
	}
}

func TestGetMe_UserNotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	app := testUserApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.NotFound) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.NotFound)
	}
}

func TestGetMe_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		ID          string  `json:"id"`
		Email       string  `json:"email"`
		Username    string  `json:"username"`
		DisplayName *string `json:"display_name"`
		AvatarKey   *string `json:"avatar_key"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	if userResp.ID != u.ID.String() {
		t.Errorf("id = %q, want %q", userResp.ID, u.ID.String())
	}
	if userResp.Email != "alice@example.com" {
		t.Errorf("email = %q, want %q", userResp.Email, "alice@example.com")
	}
	if userResp.Username != "alice" {
		t.Errorf("username = %q, want %q", userResp.Username, "alice")
	}
	if userResp.DisplayName == nil || *userResp.DisplayName != "Alice" {
		t.Errorf("display_name = %v, want %q", userResp.DisplayName, "Alice")
	}
	if userResp.AvatarKey != nil {
		t.Errorf("avatar_key = %v, want nil", userResp.AvatarKey)
	}
}

// --- UpdateMe tests ---

func TestUpdateMe_InvalidJSON(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestUpdateMe_DisplayNameTooLong(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	longName := strings.Repeat("a", 33)
	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"display_name":"`+longName+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateMe_DisplayNameEmpty(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"display_name":"   "}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateMe_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"display_name":"Bob"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		DisplayName *string `json:"display_name"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	if userResp.DisplayName == nil || *userResp.DisplayName != "Bob" {
		t.Errorf("display_name = %v, want %q", userResp.DisplayName, "Bob")
	}
}

func TestUpdateMe_EmptyBody(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		DisplayName *string `json:"display_name"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	// Display name should remain unchanged.
	if userResp.DisplayName == nil || *userResp.DisplayName != "Alice" {
		t.Errorf("display_name = %v, want %q", userResp.DisplayName, "Alice")
	}
}

func TestUpdateMe_AvatarKey(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	u := seedUser(repo)
	app := testUserApp(t, repo, u.ID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/@me", `{"avatar_key":"abc123"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var userResp struct {
		AvatarKey *string `json:"avatar_key"`
	}
	if err := json.Unmarshal(env.Data, &userResp); err != nil {
		t.Fatalf("unmarshal user response: %v", err)
	}
	if userResp.AvatarKey == nil || *userResp.AvatarKey != "abc123" {
		t.Errorf("avatar_key = %v, want %q", userResp.AvatarKey, "abc123")
	}
}
