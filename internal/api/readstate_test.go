package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/readstate"
)

type fakeReadStateRepo struct {
	ackResult *readstate.ReadState
	ackErr    error
	listErr   error
}

func (r *fakeReadStateRepo) ListByUser(_ context.Context, _ uuid.UUID) ([]readstate.ReadState, error) {
	return nil, r.listErr
}

func (r *fakeReadStateRepo) Ack(_ context.Context, _, _, _ uuid.UUID) (*readstate.ReadState, error) {
	if r.ackErr != nil {
		return nil, r.ackErr
	}
	return r.ackResult, nil
}

func (r *fakeReadStateRepo) DeleteByChannel(_ context.Context, _ uuid.UUID) error {
	return nil
}

func testReadStateApp(t *testing.T, repo readstate.Repository, userID uuid.UUID) *fiber.App {
	t.Helper()
	handler := NewReadStateHandler(repo, nil, zerolog.Nop())
	app := fiber.New()
	app.Use(fakeAuth(userID))
	app.Post("/channels/:channelID/ack", handler.Ack)
	return app
}

func TestAck_Success(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	channelID := uuid.New()
	messageID := uuid.New()

	repo := &fakeReadStateRepo{
		ackResult: &readstate.ReadState{
			UserID:        userID,
			ChannelID:     channelID,
			LastMessageID: &messageID,
			MentionCount:  0,
			UpdatedAt:     time.Now(),
		},
	}

	app := testReadStateApp(t, repo, userID)
	body := fmt.Sprintf(`{"message_id":"%s"}`, messageID)
	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/ack", body))
	respBody := readBody(t, resp)

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d; body = %s", resp.StatusCode, fiber.StatusOK, respBody)
	}

	env := parseSuccess(t, respBody)
	var rs struct {
		ChannelID     string  `json:"channel_id"`
		LastMessageID *string `json:"last_message_id"`
		MentionCount  int     `json:"mention_count"`
	}
	if err := json.Unmarshal(env.Data, &rs); err != nil {
		t.Fatalf("unmarshal read state: %v", err)
	}
	if rs.ChannelID != channelID.String() {
		t.Errorf("channel_id = %q, want %q", rs.ChannelID, channelID.String())
	}
	if rs.LastMessageID == nil || *rs.LastMessageID != messageID.String() {
		t.Errorf("last_message_id = %v, want %q", rs.LastMessageID, messageID.String())
	}
	if rs.MentionCount != 0 {
		t.Errorf("mention_count = %d, want 0", rs.MentionCount)
	}
}

func TestAck_InvalidChannelID(t *testing.T) {
	t.Parallel()

	repo := &fakeReadStateRepo{}
	app := testReadStateApp(t, repo, uuid.New())

	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/not-a-uuid/ack", `{"message_id":"abc"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidChannelID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidChannelID)
	}
}

func TestAck_InvalidMessageID(t *testing.T) {
	t.Parallel()

	repo := &fakeReadStateRepo{}
	app := testReadStateApp(t, repo, uuid.New())

	channelID := uuid.New()
	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/ack", `{"message_id":"not-a-uuid"}`))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidMessageID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidMessageID)
	}
}

func TestAck_InvalidBody(t *testing.T) {
	t.Parallel()

	repo := &fakeReadStateRepo{}
	app := testReadStateApp(t, repo, uuid.New())

	channelID := uuid.New()
	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/ack", "not json"))
	body := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, body)
	if env.Error.Code != string(apierrors.InvalidBody) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidBody)
	}
}

func TestAck_MessageNotInChannel(t *testing.T) {
	t.Parallel()

	repo := &fakeReadStateRepo{ackErr: readstate.ErrMessageNotInChannel}
	app := testReadStateApp(t, repo, uuid.New())

	channelID := uuid.New()
	messageID := uuid.New()
	body := fmt.Sprintf(`{"message_id":"%s"}`, messageID)
	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/ack", body))
	respBody := readBody(t, resp)

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
	env := parseError(t, respBody)
	if env.Error.Code != string(apierrors.InvalidMessageID) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InvalidMessageID)
	}
}

func TestAck_RepoError(t *testing.T) {
	t.Parallel()

	repo := &fakeReadStateRepo{ackErr: fmt.Errorf("database unavailable")}
	app := testReadStateApp(t, repo, uuid.New())

	channelID := uuid.New()
	messageID := uuid.New()
	body := fmt.Sprintf(`{"message_id":"%s"}`, messageID)
	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/ack", body))
	respBody := readBody(t, resp)

	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusInternalServerError)
	}
	env := parseError(t, respBody)
	if env.Error.Code != string(apierrors.InternalError) {
		t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.InternalError)
	}
}

func TestAck_Unauthenticated(t *testing.T) {
	t.Parallel()

	repo := &fakeReadStateRepo{}
	app := testReadStateApp(t, repo, uuid.Nil)

	channelID := uuid.New()
	messageID := uuid.New()
	body := fmt.Sprintf(`{"message_id":"%s"}`, messageID)
	resp := doReq(t, app, jsonReq(http.MethodPost, "/channels/"+channelID.String()+"/ack", body))
	respBody := readBody(t, resp)

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d; body = %s", resp.StatusCode, fiber.StatusUnauthorized, respBody)
	}
}
