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

	"github.com/uncord-chat/uncord-server/internal/media"
	"github.com/uncord-chat/uncord-server/internal/message"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/thread"
)

// fakeThreadRepo implements thread.Repository for handler tests.
type fakeThreadRepo struct {
	threads []thread.Thread
}

func newFakeThreadRepo() *fakeThreadRepo {
	return &fakeThreadRepo{}
}

func (r *fakeThreadRepo) Create(_ context.Context, params thread.CreateParams) (*thread.Thread, error) {
	for i := range r.threads {
		if r.threads[i].ParentMessageID == params.ParentMessageID {
			return nil, thread.ErrAlreadyExists
		}
	}
	now := time.Now()
	t := thread.Thread{
		ID:              uuid.New(),
		ChannelID:       params.ChannelID,
		ParentMessageID: params.ParentMessageID,
		Name:            params.Name,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	r.threads = append(r.threads, t)
	return &t, nil
}

func (r *fakeThreadRepo) GetByID(_ context.Context, id uuid.UUID) (*thread.Thread, error) {
	for i := range r.threads {
		if r.threads[i].ID == id {
			return &r.threads[i], nil
		}
	}
	return nil, thread.ErrNotFound
}

func (r *fakeThreadRepo) ListByChannel(_ context.Context, channelID uuid.UUID, _ *uuid.UUID, _ int) ([]thread.Thread, error) {
	var result []thread.Thread
	for i := range r.threads {
		if r.threads[i].ChannelID == channelID {
			result = append(result, r.threads[i])
		}
	}
	return result, nil
}

func (r *fakeThreadRepo) Update(_ context.Context, id uuid.UUID, params thread.UpdateParams) (*thread.Thread, error) {
	for i := range r.threads {
		if r.threads[i].ID == id {
			unlocking := params.Locked != nil && !*params.Locked
			if r.threads[i].Locked && !unlocking {
				return nil, thread.ErrLocked
			}
			if params.Name != nil {
				r.threads[i].Name = *params.Name
			}
			if params.Archived != nil {
				r.threads[i].Archived = *params.Archived
			}
			if params.Locked != nil {
				r.threads[i].Locked = *params.Locked
			}
			r.threads[i].UpdatedAt = time.Now()
			return &r.threads[i], nil
		}
	}
	return nil, thread.ErrNotFound
}

func seedThread(repo *fakeThreadRepo, channelID, parentMessageID uuid.UUID) *thread.Thread {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t := thread.Thread{
		ID:              uuid.New(),
		ChannelID:       channelID,
		ParentMessageID: parentMessageID,
		Name:            "Test Thread",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	repo.threads = append(repo.threads, t)
	return &t
}

func testThreadApp(
	t *testing.T,
	threadRepo thread.Repository,
	msgRepo message.Repository,
	resolver *permission.Resolver,
	userID uuid.UUID,
) *fiber.App {
	t.Helper()
	storage, err := media.NewLocalStorage(t.TempDir(), "http://localhost:8080")
	if err != nil {
		t.Fatalf("NewLocalStorage() error: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	handler := NewThreadHandler(threadRepo, msgRepo, nil, newFakeAttachmentRepo(), &fakeReactionRepo{}, storage, resolver, nil, nil, testMaxContent, 10, zerolog.Nop())
	app := fiber.New()
	app.Use(fakeAuth(userID))

	// Thread creation via message
	app.Post("/messages/:messageID/threads", handler.CreateThread)

	// Channel-scoped thread listing
	app.Get("/channels/:channelID/threads", handler.ListThreads)

	// Thread-scoped routes
	app.Get("/threads/:threadID", handler.GetThread)
	app.Patch("/threads/:threadID", handler.UpdateThread)
	app.Get("/threads/:threadID/messages", handler.ListThreadMessages)
	app.Post("/threads/:threadID/messages", handler.CreateThreadMessage)

	return app
}

// --- CreateThread tests ---

func TestCreateThread_Success(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	parentMsg := seedMessage(msgRepo, channelID, userID)
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/messages/"+parentMsg.ID.String()+"/threads",
		`{"name":"My Thread"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusCreated, body)
	}

	env := parseSuccess(t, body)
	var result struct {
		ID              string `json:"id"`
		ChannelID       string `json:"channel_id"`
		ParentMessageID string `json:"parent_message_id"`
		Name            string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal thread: %v", err)
	}
	if result.Name != "My Thread" {
		t.Errorf("name = %q, want %q", result.Name, "My Thread")
	}
	if result.ChannelID != channelID.String() {
		t.Errorf("channel_id = %q, want %q", result.ChannelID, channelID.String())
	}
	if result.ParentMessageID != parentMsg.ID.String() {
		t.Errorf("parent_message_id = %q, want %q", result.ParentMessageID, parentMsg.ID.String())
	}
}

func TestCreateThread_EmptyName(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	parentMsg := seedMessage(msgRepo, channelID, userID)
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/messages/"+parentMsg.ID.String()+"/threads",
		`{"name":""}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateThread_NameTooLong(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	parentMsg := seedMessage(msgRepo, channelID, userID)
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), userID)

	longName := strings.Repeat("a", 101)
	resp := doReq(t, app, jsonReq(http.MethodPost, "/messages/"+parentMsg.ID.String()+"/threads",
		`{"name":"`+longName+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateThread_MessageNotFound(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/messages/"+uuid.New().String()+"/threads",
		`{"name":"My Thread"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownMessage) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownMessage)
	}
}

func TestCreateThread_InvalidMessageID(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/messages/not-a-uuid/threads",
		`{"name":"My Thread"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidMessageID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidMessageID)
	}
}

func TestCreateThread_PermissionDenied(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	parentMsg := seedMessage(msgRepo, channelID, userID)
	app := testThreadApp(t, threadRepo, msgRepo, denyAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/messages/"+parentMsg.ID.String()+"/threads",
		`{"name":"My Thread"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MissingPermissions) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MissingPermissions)
	}
}

func TestCreateThread_AlreadyExists(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	parentMsg := seedMessage(msgRepo, channelID, userID)
	seedThread(threadRepo, channelID, parentMsg.ID)
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/messages/"+parentMsg.ID.String()+"/threads",
		`{"name":"My Thread"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ThreadExists) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ThreadExists)
	}
}

// --- ListThreads tests ---

func TestListThreads_Success(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	seedThread(threadRepo, channelID, uuid.New())
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+channelID.String()+"/threads", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var threads []json.RawMessage
	if err := json.Unmarshal(env.Data, &threads); err != nil {
		t.Fatalf("unmarshal threads: %v", err)
	}
	if len(threads) != 1 {
		t.Errorf("got %d threads, want 1", len(threads))
	}
}

func TestListThreads_Empty(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+channelID.String()+"/threads", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var threads []json.RawMessage
	if err := json.Unmarshal(env.Data, &threads); err != nil {
		t.Fatalf("unmarshal threads: %v", err)
	}
	if len(threads) != 0 {
		t.Errorf("got %d threads, want 0", len(threads))
	}
}

// --- GetThread tests ---

func TestGetThread_Success(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	th := seedThread(threadRepo, channelID, uuid.New())
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/threads/"+th.ID.String(), ""))
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
		t.Fatalf("unmarshal thread: %v", err)
	}
	if result.ID != th.ID.String() {
		t.Errorf("id = %q, want %q", result.ID, th.ID.String())
	}
	if result.Name != "Test Thread" {
		t.Errorf("name = %q, want %q", result.Name, "Test Thread")
	}
}

func TestGetThread_NotFound(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/threads/"+uuid.New().String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownThread) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownThread)
	}
}

func TestGetThread_InvalidID(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/threads/not-a-uuid", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidThreadID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidThreadID)
	}
}

func TestGetThread_PermissionDenied(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	th := seedThread(threadRepo, channelID, uuid.New())
	app := testThreadApp(t, threadRepo, msgRepo, denyAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/threads/"+th.ID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MissingPermissions) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MissingPermissions)
	}
}

// --- UpdateThread tests ---

func TestUpdateThread_Success(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	th := seedThread(threadRepo, channelID, uuid.New())
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/threads/"+th.ID.String(),
		`{"name":"Updated Name"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal thread: %v", err)
	}
	if result.Name != "Updated Name" {
		t.Errorf("name = %q, want %q", result.Name, "Updated Name")
	}
}

func TestUpdateThread_Archive(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	th := seedThread(threadRepo, channelID, uuid.New())
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/threads/"+th.ID.String(),
		`{"archived":true}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Archived bool `json:"archived"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal thread: %v", err)
	}
	if !result.Archived {
		t.Error("archived should be true")
	}
}

func TestUpdateThread_Locked_RejectsChanges(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	seedThread(threadRepo, channelID, uuid.New())
	th := &threadRepo.threads[len(threadRepo.threads)-1]
	th.Locked = true
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/threads/"+th.ID.String(),
		`{"name":"Should Fail"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ThreadLocked) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ThreadLocked)
	}
}

func TestUpdateThread_Unlock(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	seedThread(threadRepo, channelID, uuid.New())
	th := &threadRepo.threads[len(threadRepo.threads)-1]
	th.Locked = true
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/threads/"+th.ID.String(),
		`{"locked":false}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusOK, body)
	}

	env := parseSuccess(t, body)
	var result struct {
		Locked bool `json:"locked"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal thread: %v", err)
	}
	if result.Locked {
		t.Error("locked should be false")
	}
}

func TestUpdateThread_NotFound(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/threads/"+uuid.New().String(),
		`{"name":"Updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownThread) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownThread)
	}
}

func TestUpdateThread_PermissionDenied(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	th := seedThread(threadRepo, channelID, uuid.New())
	app := testThreadApp(t, threadRepo, msgRepo, denyAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/threads/"+th.ID.String(),
		`{"name":"Updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MissingPermissions) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MissingPermissions)
	}
}

// --- ListThreadMessages tests ---

func TestListThreadMessages_Success(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	th := seedThread(threadRepo, channelID, uuid.New())

	// Seed a message in the thread.
	now := time.Now()
	msgRepo.messages = append(msgRepo.messages, message.Message{
		ID:             uuid.New(),
		ChannelID:      channelID,
		AuthorID:       uuid.New(),
		Content:        "thread message",
		ThreadID:       &th.ID,
		CreatedAt:      now,
		UpdatedAt:      now,
		AuthorUsername: "testuser",
	})

	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/threads/"+th.ID.String()+"/messages", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var msgs []struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(env.Data, &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Content != "thread message" {
		t.Errorf("content = %q, want %q", msgs[0].Content, "thread message")
	}
}

func TestListThreadMessages_ThreadNotFound(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/threads/"+uuid.New().String()+"/messages", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownThread) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownThread)
	}
}

// --- CreateThreadMessage tests ---

func TestCreateThreadMessage_Success(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	th := seedThread(threadRepo, channelID, uuid.New())
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/threads/"+th.ID.String()+"/messages",
		`{"content":"hello thread"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusCreated, body)
	}

	env := parseSuccess(t, body)
	var msg struct {
		Content  string  `json:"content"`
		ThreadID *string `json:"thread_id"`
	}
	if err := json.Unmarshal(env.Data, &msg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if msg.Content != "hello thread" {
		t.Errorf("content = %q, want %q", msg.Content, "hello thread")
	}
	if msg.ThreadID == nil || *msg.ThreadID != th.ID.String() {
		t.Errorf("thread_id = %v, want %q", msg.ThreadID, th.ID.String())
	}
}

func TestCreateThreadMessage_Archived(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	seedThread(threadRepo, channelID, uuid.New())
	th := &threadRepo.threads[len(threadRepo.threads)-1]
	th.Archived = true
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/threads/"+th.ID.String()+"/messages",
		`{"content":"hello"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ThreadArchived) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ThreadArchived)
	}
}

func TestCreateThreadMessage_Locked(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	seedThread(threadRepo, channelID, uuid.New())
	th := &threadRepo.threads[len(threadRepo.threads)-1]
	th.Locked = true
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/threads/"+th.ID.String()+"/messages",
		`{"content":"hello"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ThreadLocked) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ThreadLocked)
	}
}

func TestCreateThreadMessage_ThreadNotFound(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	app := testThreadApp(t, threadRepo, msgRepo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/threads/"+uuid.New().String()+"/messages",
		`{"content":"hello"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownThread) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownThread)
	}
}

func TestCreateThreadMessage_PermissionDenied(t *testing.T) {
	t.Parallel()
	threadRepo := newFakeThreadRepo()
	msgRepo := newFakeMessageRepo()
	channelID := uuid.New()
	th := seedThread(threadRepo, channelID, uuid.New())
	app := testThreadApp(t, threadRepo, msgRepo, denyAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/threads/"+th.ID.String()+"/messages",
		`{"content":"hello"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MissingPermissions) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MissingPermissions)
	}
}
