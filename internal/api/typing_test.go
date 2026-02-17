package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/presence"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func testTypingApp(t *testing.T, userID uuid.UUID) *fiber.App {
	t.Helper()
	rdb := newTestRedis(t)
	store := presence.NewStore(rdb)
	handler := NewTypingHandler(store, nil, zerolog.Nop())

	app := fiber.New()
	app.Use(fakeAuth(userID))
	app.Post("/channels/:channelID/typing", handler.StartTyping)
	app.Delete("/channels/:channelID/typing", handler.StopTyping)
	return app
}

func TestStartTyping_Success(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	app := testTypingApp(t, userID)

	channelID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/channels/"+channelID.String()+"/typing", nil)
	req.Header.Set("Content-Type", "application/json")
	resp := doReq(t, app, req)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
}

func TestStartTyping_InvalidChannelID(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	app := testTypingApp(t, userID)

	req := httptest.NewRequest(http.MethodPost, "/channels/not-a-uuid/typing", nil)
	req.Header.Set("Content-Type", "application/json")
	resp := doReq(t, app, req)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestStartTyping_Unauthenticated(t *testing.T) {
	t.Parallel()
	app := testTypingApp(t, uuid.Nil)

	channelID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/channels/"+channelID.String()+"/typing", nil)
	req.Header.Set("Content-Type", "application/json")
	resp := doReq(t, app, req)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestStartTyping_Dedup(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	app := testTypingApp(t, userID)

	channelID := uuid.New()
	url := "/channels/" + channelID.String() + "/typing"
	req1 := httptest.NewRequest(http.MethodPost, url, nil)
	req1.Header.Set("Content-Type", "application/json")
	resp1 := doReq(t, app, req1)

	req2 := httptest.NewRequest(http.MethodPost, url, nil)
	req2.Header.Set("Content-Type", "application/json")
	resp2 := doReq(t, app, req2)

	if resp1.StatusCode != fiber.StatusNoContent {
		t.Errorf("first request status = %d, want %d", resp1.StatusCode, fiber.StatusNoContent)
	}
	if resp2.StatusCode != fiber.StatusNoContent {
		t.Errorf("second request status = %d, want %d", resp2.StatusCode, fiber.StatusNoContent)
	}
}

func TestStopTyping_Success(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	app := testTypingApp(t, userID)

	channelID := uuid.New()
	url := "/channels/" + channelID.String() + "/typing"

	// Start typing first.
	startReq := httptest.NewRequest(http.MethodPost, url, nil)
	startReq.Header.Set("Content-Type", "application/json")
	doReq(t, app, startReq)

	// Stop typing.
	stopReq := httptest.NewRequest(http.MethodDelete, url, nil)
	resp := doReq(t, app, stopReq)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
}

func TestStopTyping_NotTyping(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	app := testTypingApp(t, userID)

	channelID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/channels/"+channelID.String()+"/typing", nil)
	resp := doReq(t, app, req)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
}

func TestStopTyping_InvalidChannelID(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	app := testTypingApp(t, userID)

	req := httptest.NewRequest(http.MethodDelete, "/channels/not-a-uuid/typing", nil)
	resp := doReq(t, app, req)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestStopTyping_Unauthenticated(t *testing.T) {
	t.Parallel()
	app := testTypingApp(t, uuid.Nil)

	channelID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/channels/"+channelID.String()+"/typing", nil)
	resp := doReq(t, app, req)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}
