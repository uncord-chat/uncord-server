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
	"github.com/uncord-chat/uncord-server/internal/channel"
	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/media"
	"github.com/uncord-chat/uncord-server/internal/message"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/presence"
	"github.com/uncord-chat/uncord-server/internal/reaction"
	"github.com/uncord-chat/uncord-server/internal/thread"
)

// ThreadHandler serves thread endpoints.
type ThreadHandler struct {
	threads        thread.Repository
	messages       message.Repository
	channels       channel.Repository
	attachments    attachment.Repository
	reactions      reaction.Repository
	storage        media.StorageProvider
	resolver       *permission.Resolver
	gateway        *gateway.Publisher
	presence       *presence.Store
	maxContent     int
	maxAttachments int
	log            zerolog.Logger
}

// NewThreadHandler creates a new thread handler.
func NewThreadHandler(
	threads thread.Repository,
	messages message.Repository,
	channels channel.Repository,
	attachments attachment.Repository,
	reactions reaction.Repository,
	storage media.StorageProvider,
	resolver *permission.Resolver,
	gw *gateway.Publisher,
	presenceStore *presence.Store,
	maxContent int,
	maxAttachments int,
	logger zerolog.Logger,
) *ThreadHandler {
	return &ThreadHandler{
		threads:        threads,
		messages:       messages,
		channels:       channels,
		attachments:    attachments,
		reactions:      reactions,
		storage:        storage,
		resolver:       resolver,
		gateway:        gw,
		presence:       presenceStore,
		maxContent:     maxContent,
		maxAttachments: maxAttachments,
		log:            logger,
	}
}

// CreateThread handles POST /api/v1/messages/:messageID/threads.
func (h *ThreadHandler) CreateThread(c fiber.Ctx) error {
	messageID, err := uuid.Parse(c.Params("messageID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidMessageID, "Invalid message ID format")
	}

	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	var body models.CreateThreadRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	name, err := thread.ValidateNameRequired(body.Name)
	if err != nil {
		return mapThreadError(c, h.log, err)
	}

	parentMsg, err := h.messages.GetByID(c, messageID)
	if err != nil {
		if errors.Is(err, message.ErrNotFound) {
			return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownMessage, "Message not found")
		}
		h.log.Error().Err(err).Str("handler", "thread").Msg("fetch parent message failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	allowed, err := h.resolver.HasPermission(c, userID, parentMsg.ChannelID, permissions.CreateThreads)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "thread").Msg("permission check failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if !allowed {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.MissingPermissions,
			"You do not have permission to create threads in this channel")
	}

	t, err := h.threads.Create(c, thread.CreateParams{
		ChannelID:       parentMsg.ChannelID,
		ParentMessageID: messageID,
		Name:            name,
	})
	if err != nil {
		return mapThreadError(c, h.log, err)
	}

	result := toThreadModel(t)

	if h.gateway != nil {
		h.gateway.Enqueue(events.ThreadCreate, result)
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, result)
}

// ListThreads handles GET /api/v1/channels/:channelID/threads.
func (h *ThreadHandler) ListThreads(c fiber.Ctx) error {
	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	threads, err := h.threads.ListByChannel(c, channelID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "thread").Msg("list threads failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Thread, len(threads))
	for i := range threads {
		result[i] = toThreadModel(&threads[i])
	}
	return httputil.Success(c, result)
}

// GetThread handles GET /api/v1/threads/:threadID.
func (h *ThreadHandler) GetThread(c fiber.Ctx) error {
	threadID, err := uuid.Parse(c.Params("threadID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidThreadID, "Invalid thread ID format")
	}

	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	t, err := h.threads.GetByID(c, threadID)
	if err != nil {
		return mapThreadError(c, h.log, err)
	}

	allowed, err := h.resolver.HasPermission(c, userID, t.ChannelID, permissions.ViewChannels)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "thread").Msg("permission check failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if !allowed {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.MissingPermissions,
			"You do not have permission to view this channel")
	}

	return httputil.Success(c, toThreadModel(t))
}

// UpdateThread handles PATCH /api/v1/threads/:threadID.
func (h *ThreadHandler) UpdateThread(c fiber.Ctx) error {
	threadID, err := uuid.Parse(c.Params("threadID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidThreadID, "Invalid thread ID format")
	}

	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	t, err := h.threads.GetByID(c, threadID)
	if err != nil {
		return mapThreadError(c, h.log, err)
	}

	allowed, err := h.resolver.HasPermission(c, userID, t.ChannelID, permissions.ManageChannels)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "thread").Msg("permission check failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if !allowed {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.MissingPermissions,
			"You do not have permission to manage threads in this channel")
	}

	var body models.UpdateThreadRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	if err := thread.ValidateName(body.Name); err != nil {
		return mapThreadError(c, h.log, err)
	}

	updated, err := h.threads.Update(c, threadID, thread.UpdateParams{
		Name:     body.Name,
		Archived: body.Archived,
		Locked:   body.Locked,
	})
	if err != nil {
		return mapThreadError(c, h.log, err)
	}

	result := toThreadModel(updated)

	if h.gateway != nil {
		h.gateway.Enqueue(events.ThreadUpdate, result)
	}

	return httputil.Success(c, result)
}

// ListThreadMessages handles GET /api/v1/threads/:threadID/messages.
func (h *ThreadHandler) ListThreadMessages(c fiber.Ctx) error {
	threadID, err := uuid.Parse(c.Params("threadID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidThreadID, "Invalid thread ID format")
	}

	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	t, err := h.threads.GetByID(c, threadID)
	if err != nil {
		return mapThreadError(c, h.log, err)
	}

	allowed, err := h.resolver.HasPermission(c, userID, t.ChannelID, permissions.ViewChannels|permissions.ReadMessageHistory)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "thread").Msg("permission check failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if !allowed {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.MissingPermissions,
			"You do not have permission to read messages in this channel")
	}

	var before *uuid.UUID
	if raw := c.Query("before"); raw != "" {
		id, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid before parameter")
		}
		before = &id
	}

	rawLimit, _ := strconv.Atoi(c.Query("limit"))
	limit := message.ClampLimit(rawLimit)

	messages, err := h.messages.ListByThread(c, threadID, before, limit)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "thread").Msg("list thread messages failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	messageIDs := make([]uuid.UUID, len(messages))
	for i := range messages {
		messageIDs[i] = messages[i].ID
	}
	attachmentMap, err := h.attachments.ListByMessages(c, messageIDs)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "thread").Msg("list message attachments failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	reactionMap, err := h.reactions.SummariesByMessages(c, messageIDs)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "thread").Msg("list message reactions failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	userReactions, err := h.reactions.UserReactionsByMessages(c, messageIDs, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "thread").Msg("load user reactions failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Message, len(messages))
	for i := range messages {
		result[i] = buildMessageModel(&messages[i], attachmentMap[messages[i].ID], reactionMap[messages[i].ID], userReactions[messages[i].ID], h.storage)
	}
	return httputil.Success(c, result)
}

// CreateThreadMessage handles POST /api/v1/threads/:threadID/messages.
func (h *ThreadHandler) CreateThreadMessage(c fiber.Ctx) error {
	threadID, err := uuid.Parse(c.Params("threadID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidThreadID, "Invalid thread ID format")
	}

	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	t, err := h.threads.GetByID(c, threadID)
	if err != nil {
		return mapThreadError(c, h.log, err)
	}

	if t.Archived {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.ThreadArchived, "This thread is archived")
	}
	if t.Locked {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.ThreadLocked, "This thread is locked")
	}

	allowed, err := h.resolver.HasPermission(c, userID, t.ChannelID, permissions.SendMessagesThreads)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "thread").Msg("permission check failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if !allowed {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.MissingPermissions,
			"You do not have permission to send messages in threads")
	}

	var body models.CreateMessageRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	hasAttachments := len(body.AttachmentIDs) > 0

	if len(body.AttachmentIDs) > h.maxAttachments {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError,
			fmt.Sprintf("Too many attachments (maximum %d)", h.maxAttachments))
	}

	var attachmentIDs []uuid.UUID
	for _, raw := range body.AttachmentIDs {
		id, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid attachment_ids format")
		}
		attachmentIDs = append(attachmentIDs, id)
	}

	content, err := message.ValidateContent(body.Content, h.maxContent)
	if err != nil {
		if errors.Is(err, message.ErrEmptyContent) && hasAttachments {
			content = ""
		} else {
			return mapMessageError(c, err, h.log)
		}
	}

	var replyToID *uuid.UUID
	if body.ReplyToID != nil {
		parsed, parseErr := uuid.Parse(*body.ReplyToID)
		if parseErr != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid reply_to_id format")
		}
		replyToID = &parsed
	}

	msg, err := h.messages.Create(c, message.CreateParams{
		ChannelID: t.ChannelID,
		AuthorID:  userID,
		Content:   content,
		ReplyToID: replyToID,
		ThreadID:  &threadID,
	})
	if err != nil {
		return mapMessageError(c, err, h.log)
	}

	var linked []attachment.Attachment
	if len(attachmentIDs) > 0 {
		linked, err = h.attachments.LinkToMessage(c, attachmentIDs, msg.ID, userID)
		if err != nil {
			return mapAttachmentError(c, err, h.log)
		}
	}

	result := buildMessageModel(msg, linked, nil, nil, h.storage)

	if h.gateway != nil {
		h.gateway.Enqueue(events.MessageCreate, result)
	}

	if h.presence != nil && h.gateway != nil {
		go func() {
			existed, cErr := h.presence.ClearTyping(context.Background(), t.ChannelID, userID)
			if cErr != nil {
				h.log.Warn().Err(cErr).Msg("Failed to clear typing on thread message send")
				return
			}
			if existed {
				h.gateway.Enqueue(events.TypingStop, models.TypingStopData{
					ChannelID: t.ChannelID.String(),
					UserID:    userID.String(),
				})
			}
		}()
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, result)
}

// toThreadModel converts the internal thread type to the protocol response type.
func toThreadModel(t *thread.Thread) models.Thread {
	return models.Thread{
		ID:              t.ID.String(),
		ChannelID:       t.ChannelID.String(),
		ParentMessageID: t.ParentMessageID.String(),
		Name:            t.Name,
		Archived:        t.Archived,
		Locked:          t.Locked,
		CreatedAt:       t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       t.UpdatedAt.Format(time.RFC3339),
	}
}

// mapThreadError converts thread-layer errors to appropriate HTTP responses.
func mapThreadError(c fiber.Ctx, log zerolog.Logger, err error) error {
	switch {
	case errors.Is(err, thread.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownThread, "Thread not found")
	case errors.Is(err, thread.ErrAlreadyExists):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.ThreadExists, "A thread already exists for this message")
	case errors.Is(err, thread.ErrArchived):
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.ThreadArchived, "This thread is archived")
	case errors.Is(err, thread.ErrLocked):
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.ThreadLocked, "This thread is locked")
	case errors.Is(err, thread.ErrNameLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, thread.ErrMessageNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownMessage, "Parent message not found")
	default:
		log.Error().Err(err).Str("handler", "thread").Msg("unhandled thread service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
