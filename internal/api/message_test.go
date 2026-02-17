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

	"github.com/uncord-chat/uncord-server/internal/attachment"
	"github.com/uncord-chat/uncord-server/internal/media"
	"github.com/uncord-chat/uncord-server/internal/message"
	"github.com/uncord-chat/uncord-server/internal/permission"
)

const testMaxContent = 4000

// fakeMessageRepo implements message.Repository for handler tests.
type fakeMessageRepo struct {
	messages []message.Message
}

func newFakeMessageRepo() *fakeMessageRepo {
	return &fakeMessageRepo{}
}

func (r *fakeMessageRepo) Create(_ context.Context, params message.CreateParams) (*message.Message, error) {
	if params.ReplyToID != nil {
		found := false
		for i := range r.messages {
			if r.messages[i].ID == *params.ReplyToID && r.messages[i].ChannelID == params.ChannelID && !r.messages[i].Deleted {
				found = true
				break
			}
		}
		if !found {
			return nil, message.ErrReplyNotFound
		}
	}

	now := time.Now()
	msg := message.Message{
		ID:                uuid.New(),
		ChannelID:         params.ChannelID,
		AuthorID:          params.AuthorID,
		Content:           params.Content,
		ReplyToID:         params.ReplyToID,
		CreatedAt:         now,
		UpdatedAt:         now,
		AuthorUsername:    "testuser",
		AuthorDisplayName: nil,
		AuthorAvatarKey:   nil,
	}
	r.messages = append(r.messages, msg)
	return &msg, nil
}

func (r *fakeMessageRepo) GetByID(_ context.Context, id uuid.UUID) (*message.Message, error) {
	for i := range r.messages {
		if r.messages[i].ID == id && !r.messages[i].Deleted {
			return &r.messages[i], nil
		}
	}
	return nil, message.ErrNotFound
}

func (r *fakeMessageRepo) List(_ context.Context, channelID uuid.UUID, before *uuid.UUID, limit int) ([]message.Message, error) {
	var result []message.Message
	for i := len(r.messages) - 1; i >= 0; i-- {
		msg := r.messages[i]
		if msg.ChannelID != channelID || msg.Deleted {
			continue
		}
		if before != nil {
			var beforeTime time.Time
			for j := range r.messages {
				if r.messages[j].ID == *before {
					beforeTime = r.messages[j].CreatedAt
					break
				}
			}
			if !msg.CreatedAt.Before(beforeTime) {
				continue
			}
		}
		result = append(result, msg)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (r *fakeMessageRepo) Update(_ context.Context, id uuid.UUID, content string) (*message.Message, error) {
	for i := range r.messages {
		if r.messages[i].ID == id && !r.messages[i].Deleted {
			r.messages[i].Content = content
			now := time.Now()
			r.messages[i].EditedAt = &now
			return &r.messages[i], nil
		}
	}
	return nil, message.ErrNotFound
}

func (r *fakeMessageRepo) SoftDelete(_ context.Context, id uuid.UUID) error {
	for i := range r.messages {
		if r.messages[i].ID == id && !r.messages[i].Deleted {
			r.messages[i].Deleted = true
			return nil
		}
	}
	return message.ErrNotFound
}

func seedMessage(repo *fakeMessageRepo, channelID, authorID uuid.UUID) *message.Message {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := message.Message{
		ID:             uuid.New(),
		ChannelID:      channelID,
		AuthorID:       authorID,
		Content:        "hello world",
		CreatedAt:      now,
		UpdatedAt:      now,
		AuthorUsername: "testuser",
	}
	repo.messages = append(repo.messages, msg)
	return &msg
}

// fakeAttachmentRepo implements attachment.Repository for handler tests.
type fakeAttachmentRepo struct {
	attachments []attachment.Attachment
}

func newFakeAttachmentRepo() *fakeAttachmentRepo {
	return &fakeAttachmentRepo{}
}

func (r *fakeAttachmentRepo) Create(_ context.Context, params attachment.CreateParams) (*attachment.Attachment, error) {
	a := attachment.Attachment{
		ID:          uuid.New(),
		ChannelID:   params.ChannelID,
		UploaderID:  params.UploaderID,
		Filename:    params.Filename,
		ContentType: params.ContentType,
		SizeBytes:   params.SizeBytes,
		StorageKey:  params.StorageKey,
		Width:       params.Width,
		Height:      params.Height,
		CreatedAt:   time.Now(),
	}
	r.attachments = append(r.attachments, a)
	return &a, nil
}

func (r *fakeAttachmentRepo) GetByID(_ context.Context, id uuid.UUID) (*attachment.Attachment, error) {
	for i := range r.attachments {
		if r.attachments[i].ID == id {
			return &r.attachments[i], nil
		}
	}
	return nil, attachment.ErrNotFound
}

func (r *fakeAttachmentRepo) LinkToMessage(_ context.Context, ids []uuid.UUID, messageID uuid.UUID, uploaderID uuid.UUID) ([]attachment.Attachment, error) {
	var result []attachment.Attachment
	for _, id := range ids {
		for i := range r.attachments {
			a := &r.attachments[i]
			if a.ID == id && a.UploaderID == uploaderID && a.MessageID == nil {
				a.MessageID = &messageID
				result = append(result, *a)
			}
		}
	}
	if len(result) != len(ids) {
		return nil, attachment.ErrNotFound
	}
	return result, nil
}

func (r *fakeAttachmentRepo) ListByMessage(_ context.Context, messageID uuid.UUID) ([]attachment.Attachment, error) {
	var result []attachment.Attachment
	for i := range r.attachments {
		if r.attachments[i].MessageID != nil && *r.attachments[i].MessageID == messageID {
			result = append(result, r.attachments[i])
		}
	}
	return result, nil
}

func (r *fakeAttachmentRepo) ListByMessages(_ context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]attachment.Attachment, error) {
	result := make(map[uuid.UUID][]attachment.Attachment)
	for _, mid := range messageIDs {
		for i := range r.attachments {
			if r.attachments[i].MessageID != nil && *r.attachments[i].MessageID == mid {
				result[mid] = append(result[mid], r.attachments[i])
			}
		}
	}
	return result, nil
}

func (r *fakeAttachmentRepo) SetThumbnailKey(_ context.Context, id uuid.UUID, key string) error {
	for i := range r.attachments {
		if r.attachments[i].ID == id {
			r.attachments[i].ThumbnailKey = &key
			return nil
		}
	}
	return attachment.ErrNotFound
}

func (r *fakeAttachmentRepo) PurgeOrphans(_ context.Context, _ time.Time) ([]string, error) {
	return nil, nil
}

func testMessageApp(t *testing.T, repo message.Repository, resolver *permission.Resolver, userID uuid.UUID) *fiber.App {
	t.Helper()
	return testMessageAppWithAttachments(t, repo, newFakeAttachmentRepo(), resolver, userID)
}

func testMessageAppWithAttachments(
	t *testing.T,
	repo message.Repository,
	attachRepo attachment.Repository,
	resolver *permission.Resolver,
	userID uuid.UUID,
) *fiber.App {
	t.Helper()
	storage, err := media.NewLocalStorage(t.TempDir(), "http://localhost:8080")
	if err != nil {
		t.Fatalf("NewLocalStorage() error: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	handler := NewMessageHandler(repo, attachRepo, storage, resolver, nil, nil, nil, testMaxContent, 10, zerolog.Nop())
	app := fiber.New()

	app.Use(fakeAuth(userID))

	// Channel-scoped routes
	app.Get("/channels/:channelID/messages", handler.ListMessages)
	app.Post("/channels/:channelID/messages", handler.CreateMessage)

	// Message-scoped routes
	app.Patch("/messages/:messageID", handler.EditMessage)
	app.Delete("/messages/:messageID", handler.DeleteMessage)

	return app
}

// --- List tests ---

func TestListMessages_Empty(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	channelID := uuid.New()
	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+channelID.String()+"/messages", ""))
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
		t.Errorf("got %d messages, want 0", len(msgs))
	}
}

func TestListMessages_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	authorID := uuid.New()
	seedMessage(repo, channelID, authorID)
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+channelID.String()+"/messages", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var msgs []struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(env.Data, &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Content != "hello world" {
		t.Errorf("content = %q, want %q", msgs[0].Content, "hello world")
	}
}

func TestListMessages_WithLimit(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	authorID := uuid.New()
	for range 5 {
		seedMessage(repo, channelID, authorID)
	}
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+channelID.String()+"/messages?limit=2", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var msgs []json.RawMessage
	if err := json.Unmarshal(env.Data, &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("got %d messages, want 2", len(msgs))
	}
}

func TestListMessages_InvalidChannelID(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/not-a-uuid/messages", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidChannelID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidChannelID)
	}
}

func TestListMessages_InvalidBeforeParam(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodGet, "/channels/"+channelID.String()+"/messages?before=not-a-uuid", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

// --- Create tests ---

func TestCreateMessage_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/messages",
		`{"content":"hello"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusCreated)
	}

	env := parseSuccess(t, body)
	var msg struct {
		ID          string            `json:"id"`
		Content     string            `json:"content"`
		ChannelID   string            `json:"channel_id"`
		Attachments []json.RawMessage `json:"attachments"`
		Author      struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	if err := json.Unmarshal(env.Data, &msg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if msg.Content != "hello" {
		t.Errorf("content = %q, want %q", msg.Content, "hello")
	}
	if msg.ChannelID != channelID.String() {
		t.Errorf("channel_id = %q, want %q", msg.ChannelID, channelID.String())
	}
	if msg.ID == "" {
		t.Error("id is empty")
	}
	if len(msg.Attachments) != 0 {
		t.Errorf("attachments = %v, want empty", msg.Attachments)
	}
	if msg.Author.Username != "testuser" {
		t.Errorf("author.username = %q, want %q", msg.Author.Username, "testuser")
	}
}

func TestCreateMessage_WithReplyTo(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	authorID := uuid.New()
	original := seedMessage(repo, channelID, authorID)
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/messages",
		`{"content":"reply","reply_to_id":"`+original.ID.String()+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusCreated)
	}

	env := parseSuccess(t, body)
	var msg struct {
		ReplyToID *string `json:"reply_to_id"`
	}
	if err := json.Unmarshal(env.Data, &msg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if msg.ReplyToID == nil || *msg.ReplyToID != original.ID.String() {
		t.Errorf("reply_to_id = %v, want %q", msg.ReplyToID, original.ID.String())
	}
}

func TestCreateMessage_EmptyContent(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/messages",
		`{"content":""}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateMessage_ContentTooLong(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	longContent := strings.Repeat("a", testMaxContent+1)
	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/messages",
		`{"content":"`+longContent+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateMessage_InvalidBody(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/messages", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestCreateMessage_InvalidReplyToID(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/messages",
		`{"content":"hello","reply_to_id":"not-a-uuid"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestCreateMessage_ReplyToNonexistent(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/messages",
		`{"content":"hello","reply_to_id":"`+uuid.New().String()+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownMessage) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownMessage)
	}
}

// --- Edit tests ---

func TestEditMessage_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	msg := seedMessage(repo, channelID, userID)
	app := testMessageApp(t, repo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/messages/"+msg.ID.String(),
		`{"content":"updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	env := parseSuccess(t, body)
	var result struct {
		Content  string  `json:"content"`
		EditedAt *string `json:"edited_at"`
	}
	if err := json.Unmarshal(env.Data, &result); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if result.Content != "updated" {
		t.Errorf("content = %q, want %q", result.Content, "updated")
	}
	if result.EditedAt == nil {
		t.Error("edited_at should be set after edit")
	}
}

func TestEditMessage_NotAuthor(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	authorID := uuid.New()
	otherUserID := uuid.New()
	msg := seedMessage(repo, channelID, authorID)
	app := testMessageApp(t, repo, allowAllResolver(), otherUserID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/messages/"+msg.ID.String(),
		`{"content":"updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MissingPermissions) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MissingPermissions)
	}
}

func TestEditMessage_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/messages/"+uuid.New().String(),
		`{"content":"updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownMessage) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownMessage)
	}
}

func TestEditMessage_EmptyContent(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	msg := seedMessage(repo, channelID, userID)
	app := testMessageApp(t, repo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/messages/"+msg.ID.String(),
		`{"content":""}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestEditMessage_ContentTooLong(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	msg := seedMessage(repo, channelID, userID)
	app := testMessageApp(t, repo, allowAllResolver(), userID)

	longContent := strings.Repeat("a", testMaxContent+1)
	resp := doReq(t, app, jsonReq(http.MethodPatch, "/messages/"+msg.ID.String(),
		`{"content":"`+longContent+`"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

func TestEditMessage_InvalidID(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPatch, "/messages/not-a-uuid",
		`{"content":"updated"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

// --- Delete tests ---

func TestDeleteMessage_OwnMessage(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	userID := uuid.New()
	msg := seedMessage(repo, channelID, userID)
	app := testMessageApp(t, repo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/messages/"+msg.ID.String(), ""))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
}

func TestDeleteMessage_WithManageMessages(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	authorID := uuid.New()
	moderatorID := uuid.New()
	msg := seedMessage(repo, channelID, authorID)

	// allowAllResolver grants all permissions, so the moderator has ManageMessages.
	app := testMessageApp(t, repo, allowAllResolver(), moderatorID)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/messages/"+msg.ID.String(), ""))
	_ = readBody(t, resp)

	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
}

func TestDeleteMessage_WithoutManageMessages(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	channelID := uuid.New()
	authorID := uuid.New()
	otherUserID := uuid.New()
	msg := seedMessage(repo, channelID, authorID)

	// denyAllResolver denies all permissions.
	app := testMessageApp(t, repo, denyAllResolver(), otherUserID)

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/messages/"+msg.ID.String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.MissingPermissions) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.MissingPermissions)
	}
}

func TestDeleteMessage_NotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/messages/"+uuid.New().String(), ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownMessage) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownMessage)
	}
}

func TestDeleteMessage_InvalidID(t *testing.T) {
	t.Parallel()
	repo := newFakeMessageRepo()
	app := testMessageApp(t, repo, allowAllResolver(), uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodDelete, "/messages/not-a-uuid", ""))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}

// --- Attachment integration tests ---

func TestCreateMessage_WithAttachments(t *testing.T) {
	t.Parallel()
	msgRepo := newFakeMessageRepo()
	attRepo := newFakeAttachmentRepo()
	channelID := uuid.New()
	userID := uuid.New()

	// Seed a pending attachment.
	att, err := attRepo.Create(context.Background(), attachment.CreateParams{
		ChannelID:   channelID,
		UploaderID:  userID,
		Filename:    "photo.jpg",
		ContentType: "image/jpeg",
		SizeBytes:   1024,
		StorageKey:  "attachments/" + channelID.String() + "/abc.jpg",
	})
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}

	app := testMessageAppWithAttachments(t, msgRepo, attRepo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/messages",
		`{"content":"look at this","attachment_ids":["`+att.ID.String()+`"]}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusCreated, body)
	}

	env := parseSuccess(t, body)
	var msg struct {
		Attachments []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(env.Data, &msg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("got %d attachments, want 1", len(msg.Attachments))
	}
	if msg.Attachments[0].Filename != "photo.jpg" {
		t.Errorf("filename = %q, want %q", msg.Attachments[0].Filename, "photo.jpg")
	}
}

func TestCreateMessage_AttachmentOnly(t *testing.T) {
	t.Parallel()
	msgRepo := newFakeMessageRepo()
	attRepo := newFakeAttachmentRepo()
	channelID := uuid.New()
	userID := uuid.New()

	att, err := attRepo.Create(context.Background(), attachment.CreateParams{
		ChannelID:   channelID,
		UploaderID:  userID,
		Filename:    "doc.pdf",
		ContentType: "application/pdf",
		SizeBytes:   2048,
		StorageKey:  "attachments/" + channelID.String() + "/def.pdf",
	})
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}

	app := testMessageAppWithAttachments(t, msgRepo, attRepo, allowAllResolver(), userID)

	// Empty content, only attachment IDs.
	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/messages",
		`{"content":"","attachment_ids":["`+att.ID.String()+`"]}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, fiber.StatusCreated, body)
	}
}

func TestCreateMessage_UnknownAttachment(t *testing.T) {
	t.Parallel()
	msgRepo := newFakeMessageRepo()
	attRepo := newFakeAttachmentRepo()
	channelID := uuid.New()
	userID := uuid.New()

	app := testMessageAppWithAttachments(t, msgRepo, attRepo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/messages",
		`{"content":"hello","attachment_ids":["`+uuid.New().String()+`"]}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.UnknownAttachment) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.UnknownAttachment)
	}
}

func TestCreateMessage_TooManyAttachments(t *testing.T) {
	t.Parallel()
	msgRepo := newFakeMessageRepo()
	attRepo := newFakeAttachmentRepo()
	channelID := uuid.New()
	userID := uuid.New()

	// Create 11 IDs to exceed the limit of 10.
	ids := "["
	for i := range 11 {
		if i > 0 {
			ids += ","
		}
		ids += `"` + uuid.New().String() + `"`
	}
	ids += "]"

	app := testMessageAppWithAttachments(t, msgRepo, attRepo, allowAllResolver(), userID)

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/messages",
		`{"content":"hello","attachment_ids":`+ids+`}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.ValidationError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.ValidationError)
	}
}
