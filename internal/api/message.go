package api

import (
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/message"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/typesense"
)

// MessageHandler serves message endpoints.
type MessageHandler struct {
	messages   message.Repository
	resolver   *permission.Resolver
	indexer    *typesense.Indexer
	gateway    *gateway.Publisher
	maxContent int
	log        zerolog.Logger
}

// NewMessageHandler creates a new message handler.
func NewMessageHandler(
	messages message.Repository,
	resolver *permission.Resolver,
	indexer *typesense.Indexer,
	gw *gateway.Publisher,
	maxContent int,
	logger zerolog.Logger,
) *MessageHandler {
	return &MessageHandler{
		messages:   messages,
		resolver:   resolver,
		indexer:    indexer,
		gateway:    gw,
		maxContent: maxContent,
		log:        logger,
	}
}

// ListMessages handles GET /api/v1/channels/:channelID/messages.
func (h *MessageHandler) ListMessages(c fiber.Ctx) error {
	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	var before *uuid.UUID
	if raw := c.Query("before"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid before parameter")
		}
		before = &id
	}

	rawLimit, _ := strconv.Atoi(c.Query("limit"))
	limit := message.ClampLimit(rawLimit)

	messages, err := h.messages.List(c, channelID, before, limit)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "message").Msg("list messages failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Message, len(messages))
	for i := range messages {
		result[i] = toMessageModel(&messages[i])
	}
	return httputil.Success(c, result)
}

// CreateMessage handles POST /api/v1/channels/:channelID/messages.
func (h *MessageHandler) CreateMessage(c fiber.Ctx) error {
	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	var body models.CreateMessageRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	content, err := message.ValidateContent(body.Content, h.maxContent)
	if err != nil {
		return h.mapMessageError(c, err)
	}

	var replyToID *uuid.UUID
	if body.ReplyToID != nil {
		parsed, err := uuid.Parse(*body.ReplyToID)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid reply_to_id format")
		}
		replyToID = &parsed
	}

	msg, err := h.messages.Create(c, message.CreateParams{
		ChannelID: channelID,
		AuthorID:  userID,
		Content:   content,
		ReplyToID: replyToID,
	})
	if err != nil {
		return h.mapMessageError(c, err)
	}

	result := toMessageModel(msg)

	// Best-effort Typesense indexing.
	if h.indexer != nil {
		go func() {
			if err := h.indexer.IndexMessage(
				c, msg.ID.String(), msg.Content, msg.AuthorID.String(),
				msg.ChannelID.String(), msg.CreatedAt.Unix(),
			); err != nil {
				h.log.Warn().Err(err).Str("message_id", msg.ID.String()).Msg("Typesense index failed")
			}
		}()
	}

	// Best-effort gateway event publish.
	if h.gateway != nil {
		go func() {
			if err := h.gateway.Publish(c, events.MessageCreate, result); err != nil {
				h.log.Warn().Err(err).Str("message_id", msg.ID.String()).Msg("Gateway publish failed")
			}
		}()
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, result)
}

// EditMessage handles PATCH /api/v1/messages/:messageID.
func (h *MessageHandler) EditMessage(c fiber.Ctx) error {
	messageID, err := uuid.Parse(c.Params("messageID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid message ID format")
	}

	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	var body models.UpdateMessageRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	content, err := message.ValidateContent(body.Content, h.maxContent)
	if err != nil {
		return h.mapMessageError(c, err)
	}

	existing, err := h.messages.GetByID(c, messageID)
	if err != nil {
		return h.mapMessageError(c, err)
	}

	if existing.AuthorID != userID {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.MissingPermissions, "You can only edit your own messages")
	}

	msg, err := h.messages.Update(c, messageID, content)
	if err != nil {
		return h.mapMessageError(c, err)
	}

	result := toMessageModel(msg)

	// Best-effort Typesense upsert.
	if h.indexer != nil {
		go func() {
			if err := h.indexer.UpdateMessage(c, msg.ID.String(), msg.Content); err != nil {
				h.log.Warn().Err(err).Str("message_id", msg.ID.String()).Msg("Typesense upsert failed")
			}
		}()
	}

	// Best-effort gateway event publish.
	if h.gateway != nil {
		go func() {
			if err := h.gateway.Publish(c, events.MessageUpdate, result); err != nil {
				h.log.Warn().Err(err).Str("message_id", msg.ID.String()).Msg("Gateway publish failed")
			}
		}()
	}

	return httputil.Success(c, result)
}

// DeleteMessage handles DELETE /api/v1/messages/:messageID.
func (h *MessageHandler) DeleteMessage(c fiber.Ctx) error {
	messageID, err := uuid.Parse(c.Params("messageID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid message ID format")
	}

	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	existing, err := h.messages.GetByID(c, messageID)
	if err != nil {
		return h.mapMessageError(c, err)
	}

	// The author can always delete their own messages. Other users need the ManageMessages permission on the channel.
	if existing.AuthorID != userID {
		allowed, err := h.resolver.HasPermission(c, userID, existing.ChannelID, permissions.ManageMessages)
		if err != nil {
			h.log.Error().Err(err).Str("handler", "message").Msg("permission check failed")
			return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		}
		if !allowed {
			return httputil.Fail(c, fiber.StatusForbidden, apierrors.MissingPermissions,
				"You do not have permission to delete this message")
		}
	}

	if err := h.messages.SoftDelete(c, messageID); err != nil {
		return h.mapMessageError(c, err)
	}

	// Best-effort Typesense deletion.
	if h.indexer != nil {
		go func() {
			if err := h.indexer.DeleteMessage(c, messageID.String()); err != nil {
				h.log.Warn().Err(err).Str("message_id", messageID.String()).Msg("Typesense delete failed")
			}
		}()
	}

	// Best-effort gateway event publish.
	if h.gateway != nil {
		go func() {
			deletePayload := struct {
				ID        string `json:"id"`
				ChannelID string `json:"channel_id"`
			}{
				ID:        messageID.String(),
				ChannelID: existing.ChannelID.String(),
			}
			if err := h.gateway.Publish(c, events.MessageDelete, deletePayload); err != nil {
				h.log.Warn().Err(err).Str("message_id", messageID.String()).Msg("Gateway publish failed")
			}
		}()
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// toMessageModel converts the internal message type to the protocol response type.
func toMessageModel(m *message.Message) models.Message {
	var replyToID *string
	if m.ReplyToID != nil {
		s := m.ReplyToID.String()
		replyToID = &s
	}
	var editedAt *string
	if m.EditedAt != nil {
		s := m.EditedAt.Format(time.RFC3339)
		editedAt = &s
	}
	return models.Message{
		ID:        m.ID.String(),
		ChannelID: m.ChannelID.String(),
		Author: models.MemberUser{
			ID:          m.AuthorID.String(),
			Username:    m.AuthorUsername,
			DisplayName: m.AuthorDisplayName,
			AvatarKey:   m.AuthorAvatarKey,
		},
		Content:     m.Content,
		Attachments: []string{},
		ReplyToID:   replyToID,
		Pinned:      m.Pinned,
		EditedAt:    editedAt,
		CreatedAt:   m.CreatedAt.Format(time.RFC3339),
	}
}

// mapMessageError converts message-layer errors to appropriate HTTP responses.
func (h *MessageHandler) mapMessageError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, message.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownMessage, "Message not found")
	case errors.Is(err, message.ErrContentTooLong):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, message.ErrEmptyContent):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, message.ErrReplyNotFound):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.UnknownMessage, err.Error())
	case errors.Is(err, message.ErrNotAuthor):
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.MissingPermissions, "You can only edit your own messages")
	default:
		h.log.Error().Err(err).Str("handler", "message").Msg("unhandled message service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
