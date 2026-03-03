package api

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/attachment"
	"github.com/uncord-chat/uncord-server/internal/audit"
	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/media"
	"github.com/uncord-chat/uncord-server/internal/message"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/reaction"
)

// PinHandler serves message pin/unpin endpoints.
type PinHandler struct {
	messages    message.Repository
	attachments attachment.Repository
	reactions   reaction.Repository
	storage     media.StorageProvider
	resolver    *permission.Resolver
	gateway     *gateway.Publisher
	auditLogger *audit.Logger
	log         zerolog.Logger
}

// NewPinHandler creates a new pin handler.
func NewPinHandler(
	messages message.Repository,
	attachments attachment.Repository,
	reactions reaction.Repository,
	storage media.StorageProvider,
	resolver *permission.Resolver,
	gw *gateway.Publisher,
	auditLogger *audit.Logger,
	logger zerolog.Logger,
) *PinHandler {
	return &PinHandler{
		messages:    messages,
		attachments: attachments,
		reactions:   reactions,
		storage:     storage,
		resolver:    resolver,
		gateway:     gw,
		auditLogger: auditLogger,
		log:         logger,
	}
}

// PinMessage handles PUT /api/v1/messages/:messageID/pin.
func (h *PinHandler) PinMessage(c fiber.Ctx) error {
	messageID, ok := httputil.ParseUUIDParam(c, "messageID", apierrors.InvalidMessageID)
	if !ok {
		return nil
	}

	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	existing, err := h.messages.GetByID(c, messageID)
	if err != nil {
		return mapPinError(c, h.log, err)
	}

	allowed, err := h.resolver.HasPermission(c, userID, existing.ChannelID, permissions.ManageMessages)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "pin").Msg("permission check failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if !allowed {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.MissingPermissions,
			"You do not have permission to pin messages in this channel")
	}

	msg, err := h.messages.Pin(c, messageID)
	if err != nil {
		return mapPinError(c, h.log, err)
	}

	if h.auditLogger != nil {
		go h.auditLogger.Record(context.Background(), audit.Entry{
			ActorID: audit.UUIDPtr(userID), Action: audit.MessagePin,
			TargetType: audit.Ptr("message"), TargetID: audit.UUIDPtr(messageID),
		})
	}

	result, err := h.fullMessageModel(c, msg, userID)
	if err != nil {
		return err
	}

	if h.gateway != nil {
		h.gateway.Enqueue(events.MessageUpdate, result)
	}

	return httputil.Success(c, result)
}

// UnpinMessage handles DELETE /api/v1/messages/:messageID/pin.
func (h *PinHandler) UnpinMessage(c fiber.Ctx) error {
	messageID, ok := httputil.ParseUUIDParam(c, "messageID", apierrors.InvalidMessageID)
	if !ok {
		return nil
	}

	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	existing, err := h.messages.GetByID(c, messageID)
	if err != nil {
		return mapPinError(c, h.log, err)
	}

	allowed, err := h.resolver.HasPermission(c, userID, existing.ChannelID, permissions.ManageMessages)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "pin").Msg("permission check failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if !allowed {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.MissingPermissions,
			"You do not have permission to unpin messages in this channel")
	}

	msg, err := h.messages.Unpin(c, messageID)
	if err != nil {
		return mapPinError(c, h.log, err)
	}

	if h.auditLogger != nil {
		go h.auditLogger.Record(context.Background(), audit.Entry{
			ActorID: audit.UUIDPtr(userID), Action: audit.MessageUnpin,
			TargetType: audit.Ptr("message"), TargetID: audit.UUIDPtr(messageID),
		})
	}

	result, err := h.fullMessageModel(c, msg, userID)
	if err != nil {
		return err
	}

	if h.gateway != nil {
		h.gateway.Enqueue(events.MessageUpdate, result)
	}

	return httputil.Success(c, result)
}

// ListPins handles GET /api/v1/channels/:channelID/pins.
func (h *PinHandler) ListPins(c fiber.Ctx) error {
	channelID, ok := httputil.ParseUUIDParam(c, "channelID", apierrors.InvalidChannelID)
	if !ok {
		return nil
	}

	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	messages, err := h.messages.ListPinned(c, channelID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "pin").Msg("list pinned messages failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	messageIDs := make([]uuid.UUID, len(messages))
	for i := range messages {
		messageIDs[i] = messages[i].ID
	}
	attachmentMap, err := h.attachments.ListByMessages(c, messageIDs)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "pin").Msg("list message attachments failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	reactionMap, err := h.reactions.SummariesByMessages(c, messageIDs)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "pin").Msg("list message reactions failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	userReactions, err := h.reactions.UserReactionsByMessages(c, messageIDs, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "pin").Msg("load user reactions failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Message, len(messages))
	for i := range messages {
		result[i] = buildMessageModel(&messages[i], messageEnrichment{
			Attachments: attachmentMap[messages[i].ID],
			Summaries:   reactionMap[messages[i].ID],
			MyReactions: userReactions[messages[i].ID],
		}, h.storage)
	}
	return httputil.Success(c, result)
}

// fullMessageModel loads attachments and reactions for a single message and returns the protocol model.
func (h *PinHandler) fullMessageModel(c fiber.Ctx, msg *message.Message, userID uuid.UUID) (models.Message, error) {
	attachments, err := h.attachments.ListByMessage(c, msg.ID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "pin").Msg("list message attachments failed")
		return models.Message{}, httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	msgIDs := []uuid.UUID{msg.ID}
	reactionMap, err := h.reactions.SummariesByMessages(c, msgIDs)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "pin").Msg("load message reactions failed")
		return models.Message{}, httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	userReactions, err := h.reactions.UserReactionsByMessages(c, msgIDs, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "pin").Msg("load user reactions failed")
		return models.Message{}, httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	return buildMessageModel(msg, messageEnrichment{
		Attachments: attachments,
		Summaries:   reactionMap[msg.ID],
		MyReactions: userReactions[msg.ID],
	}, h.storage), nil
}

// mapPinError converts pin-layer errors to appropriate HTTP responses.
func mapPinError(c fiber.Ctx, log zerolog.Logger, err error) error {
	switch {
	case errors.Is(err, message.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownMessage, "Message not found")
	case errors.Is(err, message.ErrAlreadyPinned):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.AlreadyPinned, "Message is already pinned")
	case errors.Is(err, message.ErrNotPinned):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.NotPinned, "Message is not pinned")
	default:
		log.Error().Err(err).Str("handler", "pin").Msg("unhandled pin service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
