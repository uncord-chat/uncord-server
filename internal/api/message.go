package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/attachment"
	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/media"
	"github.com/uncord-chat/uncord-server/internal/message"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/presence"
	"github.com/uncord-chat/uncord-server/internal/reaction"
	"github.com/uncord-chat/uncord-server/internal/typesense"
)

// MessageHandler serves message endpoints.
type MessageHandler struct {
	messages       message.Repository
	attachments    attachment.Repository
	reactions      reaction.Repository
	storage        media.StorageProvider
	resolver       *permission.Resolver
	indexer        *typesense.Indexer
	gateway        *gateway.Publisher
	presence       *presence.Store
	maxContent     int
	maxAttachments int
	log            zerolog.Logger
}

// NewMessageHandler creates a new message handler.
func NewMessageHandler(
	messages message.Repository,
	attachments attachment.Repository,
	reactions reaction.Repository,
	storage media.StorageProvider,
	resolver *permission.Resolver,
	indexer *typesense.Indexer,
	gw *gateway.Publisher,
	presenceStore *presence.Store,
	maxContent int,
	maxAttachments int,
	logger zerolog.Logger,
) *MessageHandler {
	return &MessageHandler{
		messages:       messages,
		attachments:    attachments,
		reactions:      reactions,
		storage:        storage,
		resolver:       resolver,
		indexer:        indexer,
		gateway:        gw,
		presence:       presenceStore,
		maxContent:     maxContent,
		maxAttachments: maxAttachments,
		log:            logger,
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

	// Batch-load attachments and reactions for all returned messages.
	userID, _ := c.Locals("userID").(uuid.UUID)
	messageIDs := make([]uuid.UUID, len(messages))
	for i := range messages {
		messageIDs[i] = messages[i].ID
	}
	attachmentMap, err := h.attachments.ListByMessages(c, messageIDs)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "message").Msg("list message attachments failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	reactionMap, err := h.reactions.SummariesByMessages(c, messageIDs)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "message").Msg("list message reactions failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	userReactions, err := h.reactions.UserReactionsByMessages(c, messageIDs, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "message").Msg("load user reactions failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Message, len(messages))
	for i := range messages {
		result[i] = h.toMessageModel(&messages[i], attachmentMap[messages[i].ID], reactionMap[messages[i].ID], userReactions[messages[i].ID])
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

	hasAttachments := len(body.AttachmentIDs) > 0

	// Validate attachment count.
	if len(body.AttachmentIDs) > h.maxAttachments {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError,
			fmt.Sprintf("Too many attachments (maximum %d)", h.maxAttachments))
	}

	// Parse attachment IDs upfront.
	var attachmentIDs []uuid.UUID
	for _, raw := range body.AttachmentIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid attachment_ids format")
		}
		attachmentIDs = append(attachmentIDs, id)
	}

	// Content is required only when no attachments are provided.
	content, err := message.ValidateContent(body.Content, h.maxContent)
	if err != nil {
		if errors.Is(err, message.ErrEmptyContent) && hasAttachments {
			content = ""
		} else {
			return h.mapMessageError(c, err)
		}
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

	// Link pending attachments to the new message.
	var linked []attachment.Attachment
	if len(attachmentIDs) > 0 {
		linked, err = h.attachments.LinkToMessage(c, attachmentIDs, msg.ID, userID)
		if err != nil {
			return mapAttachmentError(c, err)
		}
	}

	// Newly created messages have no reactions yet.
	result := h.toMessageModel(msg, linked, nil, nil)

	// Best-effort Typesense indexing. Uses context.Background because Fiber recycles the request context after the
	// handler returns.
	if h.indexer != nil {
		go func() {
			if err := h.indexer.IndexMessage(
				context.Background(), msg.ID.String(), msg.Content, msg.AuthorID.String(),
				msg.ChannelID.String(), msg.CreatedAt.Unix(),
			); err != nil {
				h.log.Warn().Err(err).Str("message_id", msg.ID.String()).Msg("Typesense index failed")
			}
		}()
	}

	if h.gateway != nil {
		h.gateway.Enqueue(events.MessageCreate, result)
	}

	// Clear the typing indicator now that the message has been sent. The Valkey ClearTyping call requires its own
	// goroutine because it must not block the HTTP response, but the subsequent publish uses the bounded worker pool.
	if h.presence != nil && h.gateway != nil {
		go func() {
			existed, cErr := h.presence.ClearTyping(context.Background(), channelID, userID)
			if cErr != nil {
				h.log.Warn().Err(cErr).Msg("Failed to clear typing on message send")
				return
			}
			if existed {
				h.gateway.Enqueue(events.TypingStop, models.TypingStopData{
					ChannelID: channelID.String(),
					UserID:    userID.String(),
				})
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

	attachments, err := h.attachments.ListByMessage(c, msg.ID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "message").Msg("list message attachments failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	msgIDs := []uuid.UUID{msg.ID}
	reactionMap, err := h.reactions.SummariesByMessages(c, msgIDs)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "message").Msg("load message reactions failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	userReactions, err := h.reactions.UserReactionsByMessages(c, msgIDs, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "message").Msg("load user reactions failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := h.toMessageModel(msg, attachments, reactionMap[msg.ID], userReactions[msg.ID])

	// Best-effort Typesense upsert. Uses context.Background because Fiber recycles the request context after the
	// handler returns.
	if h.indexer != nil {
		go func() {
			if err := h.indexer.UpdateMessage(context.Background(), msg.ID.String(), msg.Content); err != nil {
				h.log.Warn().Err(err).Str("message_id", msg.ID.String()).Msg("Typesense upsert failed")
			}
		}()
	}

	if h.gateway != nil {
		h.gateway.Enqueue(events.MessageUpdate, result)
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

	// Best-effort Typesense deletion. Uses context.Background because Fiber recycles the request context after the
	// handler returns.
	if h.indexer != nil {
		go func() {
			if err := h.indexer.DeleteMessage(context.Background(), messageID.String()); err != nil {
				h.log.Warn().Err(err).Str("message_id", messageID.String()).Msg("Typesense delete failed")
			}
		}()
	}

	if h.gateway != nil {
		h.gateway.Enqueue(events.MessageDelete, models.MessageDeleteData{
			ID:        messageID.String(),
			ChannelID: existing.ChannelID.String(),
		})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// toMessageModel converts the internal message type to the protocol response type. The summaries parameter contains
// grouped reaction counts for this message (nil is safe and produces an empty slice). The myReactions map contains the
// emoji keys the requesting user has reacted with (used to set the Me flag).
func (h *MessageHandler) toMessageModel(m *message.Message, attachments []attachment.Attachment, summaries []reaction.Summary, myReactions map[string]bool) models.Message {
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

	modelAttachments := make([]models.Attachment, len(attachments))
	for i := range attachments {
		modelAttachments[i] = toAttachmentModel(&attachments[i], h.storage)
	}

	modelReactions := make([]models.ReactionSummary, len(summaries))
	for i, s := range summaries {
		var emojiID *string
		var reactionKey string
		if s.EmojiID != nil {
			id := s.EmojiID.String()
			emojiID = &id
			reactionKey = "custom:" + id
		} else if s.EmojiUnicode != nil {
			reactionKey = *s.EmojiUnicode
		}
		modelReactions[i] = models.ReactionSummary{
			EmojiID:      emojiID,
			EmojiUnicode: s.EmojiUnicode,
			Count:        s.Count,
			Me:           myReactions[reactionKey],
		}
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
		Attachments: modelAttachments,
		Reactions:   modelReactions,
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
