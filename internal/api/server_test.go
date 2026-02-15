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

	"github.com/uncord-chat/uncord-server/internal/server"
)

// fakeServerRepo implements server.Repository for handler tests.
type fakeServerRepo struct {
	cfg *server.Config
}

func (r *fakeServerRepo) Get(_ context.Context) (*server.Config, error) {
	if r.cfg == nil {
		return nil, server.ErrNotFound
	}
	return r.cfg, nil
}

func (r *fakeServerRepo) Update(_ context.Context, params server.UpdateParams) (*server.Config, error) {
	if r.cfg == nil {
		return nil, server.ErrNotFound
	}
	if params.Name != nil {
		trimmed := strings.TrimSpace(*params.Name)
		r.cfg.Name = trimmed
	}
	if params.Description != nil {
		r.cfg.Description = *params.Description
	}
	if params.IconKey != nil {
		r.cfg.IconKey = params.IconKey
	}
	if params.BannerKey != nil {
		r.cfg.BannerKey = params.BannerKey
	}
	return r.cfg, nil
}

func seedServerConfig() *fakeServerRepo {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	iconKey := "icon-abc"
	return &fakeServerRepo{
		cfg: &server.Config{
			ID:          uuid.New(),
			Name:        "Test Server",
			Description: "A test server",
			IconKey:     &iconKey,
			BannerKey:   nil,
			OwnerID:     uuid.New(),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
}

func testServerApp(t *testing.T, repo server.Repository, userID uuid.UUID) *fiber.App {
	t.Helper()
	handler := NewServerHandler(repo, zerolog.Nop())
	app := fiber.New()

	app.Use(fakeAuth(userID))

	app.Get("/", handler.Get)
	app.Patch("/", handler.Update)
	return app
}

func testPublicServerInfoApp(t *testing.T, repo server.Repository) *fiber.App {
	t.Helper()
	handler := NewServerHandler(repo, zerolog.Nop())
	app := fiber.New()
	app.Get("/", handler.GetPublicInfo)
	return app
}

// --- GetPublicInfo tests ---

func TestGetPublicInfo_Success(t *testing.T) {
	t.Parallel()
	repo := seedServerConfig()
	app := testPublicServerInfoApp(t, repo)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var info struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		IconKey     *string `json:"icon_key"`
	}
	if err := json.Unmarshal(env.Data, &info); err != nil {
		t.Fatalf("unmarshal public info response: %v", err)
	}
	if info.Name != "Test Server" {
		t.Errorf("name = %q, want %q", info.Name, "Test Server")
	}
	if info.Description != "A test server" {
		t.Errorf("description = %q, want %q", info.Description, "A test server")
	}
	if info.IconKey == nil || *info.IconKey != "icon-abc" {
		t.Errorf("icon_key = %v, want %q", info.IconKey, "icon-abc")
	}

	// Verify that privileged fields are absent from the response.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("unmarshal raw data: %v", err)
	}
	for _, key := range []string{"id", "owner_id", "banner_key", "created_at", "updated_at"} {
		if _, ok := raw[key]; ok {
			t.Errorf("response contains privileged field %q", key)
		}
	}
}

func TestGetPublicInfo_NotFound(t *testing.T) {
	t.Parallel()
	repo := &fakeServerRepo{}
	app := testPublicServerInfoApp(t, repo)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.NotFound) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.NotFound)
	}
}

// --- Get tests ---

func TestGetServer_Unauthenticated(t *testing.T) {
	t.Parallel()
	repo := seedServerConfig()
	app := testServerApp(t, repo, uuid.Nil)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.Unauthorised) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.Unauthorised)
	}
}

func TestGetServer_NotFound(t *testing.T) {
	t.Parallel()
	repo := &fakeServerRepo{}
	app := testServerApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.NotFound) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.NotFound)
	}
}

func TestGetServer_Success(t *testing.T) {
	t.Parallel()
	repo := seedServerConfig()
	app := testServerApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var sc struct {
		ID          string  `json:"id"`
		Name        string  `json:"name"`
		Description string  `json:"description"`
		IconKey     *string `json:"icon_key"`
		BannerKey   *string `json:"banner_key"`
		OwnerID     string  `json:"owner_id"`
		CreatedAt   string  `json:"created_at"`
		UpdatedAt   string  `json:"updated_at"`
	}
	if err := json.Unmarshal(env.Data, &sc); err != nil {
		t.Fatalf("unmarshal server config response: %v", err)
	}
	if sc.ID != repo.cfg.ID.String() {
		t.Errorf("id = %q, want %q", sc.ID, repo.cfg.ID.String())
	}
	if sc.Name != "Test Server" {
		t.Errorf("name = %q, want %q", sc.Name, "Test Server")
	}
	if sc.Description != "A test server" {
		t.Errorf("description = %q, want %q", sc.Description, "A test server")
	}
	if sc.IconKey == nil || *sc.IconKey != "icon-abc" {
		t.Errorf("icon_key = %v, want %q", sc.IconKey, "icon-abc")
	}
	if sc.BannerKey != nil {
		t.Errorf("banner_key = %v, want nil", sc.BannerKey)
	}
	if sc.OwnerID != repo.cfg.OwnerID.String() {
		t.Errorf("owner_id = %q, want %q", sc.OwnerID, repo.cfg.OwnerID.String())
	}
	if sc.CreatedAt == "" {
		t.Error("created_at is empty")
	}
	if sc.UpdatedAt == "" {
		t.Error("updated_at is empty")
	}
}

// --- Update tests ---

func TestUpdateServer_InvalidJSON(t *testing.T) {
	t.Parallel()
	repo := seedServerConfig()
	app := testServerApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestUpdateServer_NameTooLong(t *testing.T) {
	t.Parallel()
	repo := seedServerConfig()
	app := testServerApp(t, repo, uuid.New())

	longName := strings.Repeat("a", 101)
	resp := doReq(t, app, jsonReq(http.MethodPatch, "/", `{"name":"`+longName+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateServer_NameEmptyWhitespace(t *testing.T) {
	t.Parallel()
	repo := seedServerConfig()
	app := testServerApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/", `{"name":"   "}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateServer_DescriptionTooLong(t *testing.T) {
	t.Parallel()
	repo := seedServerConfig()
	app := testServerApp(t, repo, uuid.New())

	longDesc := strings.Repeat("a", 1025)
	resp := doReq(t, app, jsonReq(http.MethodPatch, "/", `{"description":"`+longDesc+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestUpdateServer_Success(t *testing.T) {
	t.Parallel()
	repo := seedServerConfig()
	app := testServerApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/", `{"name":"New Name"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var sc struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &sc); err != nil {
		t.Fatalf("unmarshal server config response: %v", err)
	}
	if sc.Name != "New Name" {
		t.Errorf("name = %q, want %q", sc.Name, "New Name")
	}
}

func TestUpdateServer_EmptyBody(t *testing.T) {
	t.Parallel()
	repo := seedServerConfig()
	app := testServerApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/", `{}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var sc struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &sc); err != nil {
		t.Fatalf("unmarshal server config response: %v", err)
	}
	if sc.Name != "Test Server" {
		t.Errorf("name = %q, want %q (should be unchanged)", sc.Name, "Test Server")
	}
}

func TestUpdateServer_IconAndBannerKey(t *testing.T) {
	t.Parallel()
	repo := seedServerConfig()
	app := testServerApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/", `{"icon_key":"new-icon","banner_key":"new-banner"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var sc struct {
		IconKey   *string `json:"icon_key"`
		BannerKey *string `json:"banner_key"`
	}
	if err := json.Unmarshal(env.Data, &sc); err != nil {
		t.Fatalf("unmarshal server config response: %v", err)
	}
	if sc.IconKey == nil || *sc.IconKey != "new-icon" {
		t.Errorf("icon_key = %v, want %q", sc.IconKey, "new-icon")
	}
	if sc.BannerKey == nil || *sc.BannerKey != "new-banner" {
		t.Errorf("banner_key = %v, want %q", sc.BannerKey, "new-banner")
	}
}
