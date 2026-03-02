package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/media"
	"github.com/uncord-chat/uncord-server/internal/message"
	"github.com/uncord-chat/uncord-server/internal/permission"
)

func testPinApp(t *testing.T, repo message.Repository, resolver *permission.Resolver, userID uuid.UUID) *fiber.App {
	t.Helper()
	storage, err := media.NewLocalStorage(t.TempDir(), "http://localhost:8080")
	if err != nil {
		t.Fatalf("NewLocalStorage() error: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	handler := NewPinHandler(repo, newFakeAttachmentRepo(), &fakeReactionRepo{}, storage, resolver, nil, nil, zerolog.Nop())
	app := fiber.New()
	app.Use(fakeAuth(userID))

	// Pin/unpin routes
	app.Put("/messages/:messageID/pin", handler.PinMessage)
	app.Delete("/messages/:messageID/pin", handler.UnpinMessage)

	// Pinned message listing
	app.Get("/channels/:channelID/pins", handler.ListPins)

	return app
}

// --- PinMessage tests ---

func TestPinMessage_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	msg := seedMessage(repo, channelID, userID)
	app := testPinApp(t, repo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodPut, "/messages/"+msg.ID.String()+"/pin", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}

	env := parseSuccess(t, body)
	var result struct {
		ID     string `json:"id"`
		Pinned bool   `json:"pinned"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if !result.Pinned {
		t.Error("pinned should be true")
	}
	if result.ID != msg.ID.String() {
		t.Errorf("id = %q, want %q", result.ID, msg.ID.String())
	}
}

func TestPinMessage_AlreadyPinned(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	msg := seedMessage(repo, channelID, userID)
	repo.messages[len(repo.messages)-1].Pinned = true
	app := testPinApp(t, repo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodPut, "/messages/"+msg.ID.String()+"/pin", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.AlreadyPinned) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.AlreadyPinned)
	}
}

func TestPinMessage_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	app := testPinApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/messages/"+uuid.New().String()+"/pin", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownMessage) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownMessage)
	}
}

func TestPinMessage_InvalidID(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	app := testPinApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/messages/not-a-uuid/pin", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidMessageID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidMessageID)
	}
}

func TestPinMessage_PermissionDenied(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	msg := seedMessage(repo, channelID, userID)
	app := testPinApp(t, repo, denyAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPut, "/messages/"+msg.ID.String()+"/pin", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MissingPermissions) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MissingPermissions)
	}
}

// --- UnpinMessage tests ---

func TestUnpinMessage_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	msg := seedMessage(repo, channelID, userID)
	repo.messages[len(repo.messages)-1].Pinned = true
	app := testPinApp(t, repo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/messages/"+msg.ID.String()+"/pin", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}

	env := parseSuccess(t, body)
	var result struct {
		Pinned bool `json:"pinned"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if result.Pinned {
		t.Error("pinned should be false after unpin")
	}
}

func TestUnpinMessage_NotPinned(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	msg := seedMessage(repo, channelID, userID)
	app := testPinApp(t, repo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/messages/"+msg.ID.String()+"/pin", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.NotPinned) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.NotPinned)
	}
}

func TestUnpinMessage_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	app := testPinApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/messages/"+uuid.New().String()+"/pin", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownMessage) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownMessage)
	}
}

func TestUnpinMessage_PermissionDenied(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	msg := seedMessage(repo, channelID, userID)
	repo.messages[len(repo.messages)-1].Pinned = true
	app := testPinApp(t, repo, denyAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/messages/"+msg.ID.String()+"/pin", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MissingPermissions) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MissingPermissions)
	}
}

// --- ListPins tests ---

func TestListPins_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	msg := seedMessage(repo, channelID, userID)
	repo.messages[len(repo.messages)-1].Pinned = true
	// Seed an unpinned message that should not appear.
	seedMessage(repo, channelID, userID)
	app := testPinApp(t, repo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+channelID.String()+"/pins", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var msgs []struct {
		ID     string `json:"id"`
		Pinned bool   `json:"pinned"`
	}
	if err := json.Unmarshal(env.Data, &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d pinned messages, want 1", len(msgs))
	}
	if !msgs[0].Pinned {
		t.Error("pinned should be true")
	}
	if msgs[0].ID != msg.ID.String() {
		t.Errorf("id = %q, want %q", msgs[0].ID, msg.ID.String())
	}
}

func TestListPins_Empty(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	app := testPinApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+channelID.String()+"/pins", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var msgs []json.RawMessage
	if err := json.Unmarshal(env.Data, &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("got %d pinned messages, want 0", len(msgs))
	}
}

func TestListPins_InvalidChannelID(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	app := testPinApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/not-a-uuid/pins", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidChannelID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidChannelID)
	}
}
