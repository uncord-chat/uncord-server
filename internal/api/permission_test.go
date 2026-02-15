package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/permission"
)

// overrideKey uniquely identifies a permission override in the fake store.
type overrideKey struct {
	targetType    permission.TargetType
	targetID      uuid.UUID
	principalType permission.PrincipalType
	principalID   uuid.UUID
}

// fakeOverrideStore implements permission.OverrideStore for handler tests.
type fakeOverrideStore struct {
	overrides map[overrideKey]*permission.OverrideRow
}

func newFakeOverrideStore() *fakeOverrideStore {
	return &fakeOverrideStore{overrides: make(map[overrideKey]*permission.OverrideRow)}
}

func (s *fakeOverrideStore) Set(_ context.Context, targetType permission.TargetType, targetID uuid.UUID, principalType permission.PrincipalType, principalID uuid.UUID, allow, deny permissions.Permission) (*permission.OverrideRow, error) {
	key := overrideKey{targetType, targetID, principalType, principalID}
	existing, ok := s.overrides[key]
	if ok {
		existing.Allow = allow
		existing.Deny = deny
		return existing, nil
	}
	row := &permission.OverrideRow{
		ID:            uuid.New(),
		TargetType:    targetType,
		TargetID:      targetID,
		PrincipalType: principalType,
		PrincipalID:   principalID,
		Allow:         allow,
		Deny:          deny,
	}
	s.overrides[key] = row
	return row, nil
}

func (s *fakeOverrideStore) Delete(_ context.Context, targetType permission.TargetType, targetID uuid.UUID, principalType permission.PrincipalType, principalID uuid.UUID) error {
	key := overrideKey{targetType, targetID, principalType, principalID}
	if _, ok := s.overrides[key]; !ok {
		return permission.ErrOverrideNotFound
	}
	delete(s.overrides, key)
	return nil
}

func testPermissionApp(t *testing.T, overrides permission.OverrideStore, resolver *permission.Resolver, userID uuid.UUID) *fiber.App {
	t.Helper()
	handler := NewPermissionHandler(overrides, resolver, nil, zerolog.Nop())
	app := fiber.New()

	app.Use(fakeAuth(userID))

	app.Put("/channels/:channelID/overrides/:targetID", handler.SetOverride)
	app.Delete("/channels/:channelID/overrides/:targetID", handler.DeleteOverride)
	app.Get("/channels/:channelID/permissions/@me", handler.GetMyPermissions)
	return app
}

// --- SetOverride tests ---

func TestSetOverride_InvalidChannelID(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/channels/not-a-uuid/overrides/"+uuid.New().String(), `{"type":"role","allow":1,"deny":0}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidChannelID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidChannelID)
	}
}

func TestSetOverride_InvalidTargetID(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/channels/"+uuid.New().String()+"/overrides/not-a-uuid", `{"type":"role","allow":1,"deny":0}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestSetOverride_InvalidJSON(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/channels/"+uuid.New().String()+"/overrides/"+uuid.New().String(), "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestSetOverride_MissingType(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/channels/"+uuid.New().String()+"/overrides/"+uuid.New().String(), `{"allow":1,"deny":0}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestSetOverride_InvalidType(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/channels/"+uuid.New().String()+"/overrides/"+uuid.New().String(), `{"type":"banana","allow":1,"deny":0}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestSetOverride_AllowOutOfRange(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/channels/"+uuid.New().String()+"/overrides/"+uuid.New().String(), `{"type":"role","allow":1099511627776,"deny":0}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestSetOverride_DenyOutOfRange(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/channels/"+uuid.New().String()+"/overrides/"+uuid.New().String(), `{"type":"role","allow":0,"deny":1099511627776}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestSetOverride_Success(t *testing.T) {
	t.Parallel()
	store := newFakeOverrideStore()
	app := testPermissionApp(t, store, allowAllResolver(), uuid.New())

	channelID := uuid.New()
	targetID := uuid.New()

	resp := doReq(t, app, jsonReq(http.MethodPut, "/channels/"+channelID.String()+"/overrides/"+targetID.String(), `{"type":"role","allow":1,"deny":2}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		TargetID string `json:"target_id"`
		Allow    int64  `json:"allow"`
		Deny     int64  `json:"deny"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal override: %v", err)
	}
	if result.ID == "" {
		t.Error("id is empty")
	}
	if result.Type != "role" {
		t.Errorf("type = %q, want %q", result.Type, "role")
	}
	if result.TargetID != targetID.String() {
		t.Errorf("target_id = %q, want %q", result.TargetID, targetID.String())
	}
	if result.Allow != 1 {
		t.Errorf("allow = %d, want %d", result.Allow, 1)
	}
	if result.Deny != 2 {
		t.Errorf("deny = %d, want %d", result.Deny, 2)
	}
}

func TestSetOverride_UserType(t *testing.T) {
	t.Parallel()
	store := newFakeOverrideStore()
	app := testPermissionApp(t, store, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/channels/"+uuid.New().String()+"/overrides/"+uuid.New().String(), `{"type":"user","allow":4,"deny":0}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal override: %v", err)
	}
	if result.Type != "user" {
		t.Errorf("type = %q, want %q", result.Type, "user")
	}
}

func TestSetOverride_Upsert(t *testing.T) {
	t.Parallel()
	store := newFakeOverrideStore()
	app := testPermissionApp(t, store, allowAllResolver(), uuid.New())

	channelID := uuid.New()
	targetID := uuid.New()
	url := "/channels/" + channelID.String() + "/overrides/" + targetID.String()

	// First request creates the override.
	resp := doReq(t, app, jsonReq(http.MethodPut, url, `{"type":"role","allow":1,"deny":0}`))
	_ = readBody(t, resp)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("first upsert status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	// Second request updates the same override.
	resp = doReq(t, app, jsonReq(http.MethodPut, url, `{"type":"role","allow":3,"deny":4}`))
	body := readBody(t, resp)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("second upsert status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Allow int64 `json:"allow"`
		Deny  int64 `json:"deny"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal override: %v", err)
	}
	if result.Allow != 3 {
		t.Errorf("allow = %d, want %d", result.Allow, 3)
	}
	if result.Deny != 4 {
		t.Errorf("deny = %d, want %d", result.Deny, 4)
	}

	if len(store.overrides) != 1 {
		t.Errorf("store has %d overrides, want 1", len(store.overrides))
	}
}

// --- DeleteOverride tests ---

func TestDeleteOverride_InvalidChannelID(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/channels/not-a-uuid/overrides/"+uuid.New().String()+"?type=role", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidChannelID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidChannelID)
	}
}

func TestDeleteOverride_InvalidTargetID(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/channels/"+uuid.New().String()+"/overrides/not-a-uuid?type=role", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestDeleteOverride_MissingType(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/channels/"+uuid.New().String()+"/overrides/"+uuid.New().String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestDeleteOverride_InvalidType(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/channels/"+uuid.New().String()+"/overrides/"+uuid.New().String()+"?type=banana", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestDeleteOverride_NotFound(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/channels/"+uuid.New().String()+"/overrides/"+uuid.New().String()+"?type=role", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownOverride) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownOverride)
	}
}

func TestDeleteOverride_Success(t *testing.T) {
	t.Parallel()
	store := newFakeOverrideStore()
	app := testPermissionApp(t, store, allowAllResolver(), uuid.New())

	channelID := uuid.New()
	targetID := uuid.New()

	// Seed an override.
	_, _ = store.Set(context.Background(), permission.TargetChannel, channelID, permission.PrincipalRole, targetID, 1, 0)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/channels/"+channelID.String()+"/overrides/"+targetID.String()+"?type=role", ""))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
	if len(store.overrides) != 0 {
		t.Errorf("store has %d overrides, want 0", len(store.overrides))
	}
}

// --- GetMyPermissions tests ---

func TestGetMyPermissions_InvalidChannelID(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/not-a-uuid/permissions/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidChannelID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidChannelID)
	}
}

func TestGetMyPermissions_Unauthenticated(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.Nil)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+uuid.New().String()+"/permissions/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.Unauthorised) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.Unauthorised)
	}
}

func TestGetMyPermissions_Success(t *testing.T) {
	t.Parallel()
	app := testPermissionApp(t, newFakeOverrideStore(), allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+uuid.New().String()+"/permissions/@me", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Permissions int64 `json:"permissions"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal permissions: %v", err)
	}
	if result.Permissions != int64(permissions.AllPermissions) {
		t.Errorf("permissions = %d, want %d", result.Permissions, permissions.AllPermissions)
	}
}
