package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/audit"
)

// fakeAuditRepo implements audit.Repository for handler tests.
type fakeAuditRepo struct {
	entries []audit.Entry
	err     error
}

func (r *fakeAuditRepo) Create(_ context.Context, entry audit.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

func (r *fakeAuditRepo) List(_ context.Context, _ audit.ListParams) ([]audit.Entry, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.entries, nil
}

func testAuditApp(t *testing.T, repo audit.Repository, userID uuid.UUID) *fiber.App {
	t.Helper()
	handler := NewAuditHandler(repo, zerolog.Nop())
	app := fiber.New()
	app.Use(fakeAuth(userID))
	app.Get("/audit-log", handler.List)
	return app
}

func TestAuditList_Empty(t *testing.T) {
	t.Parallel()
	repo := &fakeAuditRepo{}
	app := testAuditApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/audit-log", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var entries []json.RawMessage
	if err := json.Unmarshal(env.Data, &entries); err != nil {
		t.Fatalf("unmarshal entries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestAuditList_Populated(t *testing.T) {
	t.Parallel()
	entryID := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	targetType := "role"

	repo := &fakeAuditRepo{
		entries: []audit.Entry{
			{
				ID:         entryID,
				ActorID:    &actorID,
				Action:     audit.RoleCreate,
				TargetType: &targetType,
				TargetID:   &targetID,
				Changes:    json.RawMessage(`{"name":"admin"}`),
				CreatedAt:  now,
			},
		},
	}
	app := testAuditApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/audit-log", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var entries []struct {
		ID         string `json:"id"`
		ActorID    string `json:"actor_id"`
		Action     string `json:"action"`
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
	}
	if err := json.Unmarshal(env.Data, &entries); err != nil {
		t.Fatalf("unmarshal entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].ID != entryID.String() {
		t.Errorf("id = %q, want %q", entries[0].ID, entryID.String())
	}
	if entries[0].Action != "role.create" {
		t.Errorf("action = %q, want %q", entries[0].Action, "role.create")
	}
}

func TestAuditList_InvalidActorID(t *testing.T) {
	t.Parallel()
	repo := &fakeAuditRepo{}
	app := testAuditApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/audit-log?actor_id=not-a-uuid", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestAuditList_InvalidTargetID(t *testing.T) {
	t.Parallel()
	repo := &fakeAuditRepo{}
	app := testAuditApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/audit-log?target_id=bad", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestAuditList_InvalidBefore(t *testing.T) {
	t.Parallel()
	repo := &fakeAuditRepo{}
	app := testAuditApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/audit-log?before=not-valid", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestAuditList_RepoError(t *testing.T) {
	t.Parallel()
	repo := &fakeAuditRepo{err: errors.New("db unavailable")}
	app := testAuditApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/audit-log", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusInternalServerError)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InternalError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InternalError)
	}
}

func TestAuditList_LimitClamping(t *testing.T) {
	t.Parallel()
	repo := &fakeAuditRepo{}
	app := testAuditApp(t, repo, uuid.New())

	// Request with limit=0 should use default limit (succeeds, returns empty)
	resp := doReq(t, app, jsonReq(http.MethodGet, "/audit-log?limit=0", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var entries []json.RawMessage
	if err := json.Unmarshal(env.Data, &entries); err != nil {
		t.Fatalf("unmarshal entries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}
