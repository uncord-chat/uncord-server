package api

import (
	"context"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/emoji"
	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/message"
	"github.com/uncord-chat/uncord-server/internal/reaction"
)

// ReactionHandler serves reaction endpoints.
type ReactionHandler struct {
	reactions reaction.Repository
	messages  message.Repository
	emoji     emoji.Repository
	gateway   *gateway.Publisher
	log       zerolog.Logger
}

// NewReactionHandler creates a new reaction handler.
func NewReactionHandler(
	reactions reaction.Repository,
	messages message.Repository,
	emojiRepo emoji.Repository,
	gw *gateway.Publisher,
	logger zerolog.Logger,
) *ReactionHandler {
	return &ReactionHandler{
		reactions: reactions,
		messages:  messages,
		emoji:     emojiRepo,
		gateway:   gw,
		log:       logger,
	}
}

// AddReaction handles PUT /api/v1/channels/:channelID/messages/:messageID/reactions/:emoji.
func (h *ReactionHandler) AddReaction(c fiber.Ctx) error {
	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	messageID, err := uuid.Parse(c.Params("messageID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidMessageID, "Invalid message ID format")
	}

	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	emojiID, emojiUnicode, err := parseEmojiParam(c.Params("emoji"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	}

	msg, err := h.messages.GetByID(c, messageID)
	if err != nil {
		return h.mapReactionError(c, err)
	}
	if msg.ChannelID != channelID {
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownMessage, "Message not found in this channel")
	}

	if emojiID != nil {
		if _, err := h.emoji.GetByID(c, *emojiID); err != nil {
			if errors.Is(err, emoji.ErrNotFound) {
				return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownEmoji, "Custom emoji not found")
			}
			h.log.Error().Err(err).Str("handler", "reaction").Msg("emoji lookup failed")
			return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		}
	}

	_, err = h.reactions.Add(c, messageID, userID, emojiID, emojiUnicode)
	if err != nil {
		return h.mapReactionError(c, err)
	}

	if h.gateway != nil {
		go func() {
			data := models.ReactionAddData{
				MessageID: messageID.String(),
				ChannelID: channelID.String(),
				UserID:    userID.String(),
			}
			if emojiID != nil {
				s := emojiID.String()
				data.EmojiID = &s
			}
			if emojiUnicode != nil {
				data.EmojiUnicode = emojiUnicode
			}
			if err := h.gateway.Publish(context.Background(), events.ReactionAdd, data); err != nil {
				h.log.Warn().Err(err).Msg("Failed to publish reaction add event")
			}
		}()
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// RemoveReaction handles DELETE /api/v1/channels/:channelID/messages/:messageID/reactions/:emoji.
func (h *ReactionHandler) RemoveReaction(c fiber.Ctx) error {
	_, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	messageID, err := uuid.Parse(c.Params("messageID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidMessageID, "Invalid message ID format")
	}

	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	emojiID, emojiUnicode, err := parseEmojiParam(c.Params("emoji"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	}

	if err := h.reactions.Remove(c, messageID, userID, emojiID, emojiUnicode); err != nil {
		return h.mapReactionError(c, err)
	}

	if h.gateway != nil {
		channelID, _ := uuid.Parse(c.Params("channelID"))
		go func() {
			data := models.ReactionRemoveData{
				MessageID: messageID.String(),
				ChannelID: channelID.String(),
				UserID:    userID.String(),
			}
			if emojiID != nil {
				s := emojiID.String()
				data.EmojiID = &s
			}
			if emojiUnicode != nil {
				data.EmojiUnicode = emojiUnicode
			}
			if err := h.gateway.Publish(context.Background(), events.ReactionRemove, data); err != nil {
				h.log.Warn().Err(err).Msg("Failed to publish reaction remove event")
			}
		}()
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ListReactions handles GET /api/v1/channels/:channelID/messages/:messageID/reactions.
func (h *ReactionHandler) ListReactions(c fiber.Ctx) error {
	_, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	messageID, err := uuid.Parse(c.Params("messageID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidMessageID, "Invalid message ID format")
	}

	userID, _ := c.Locals("userID").(uuid.UUID)

	reactions, err := h.reactions.ListByMessage(c, messageID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "reaction").Msg("list reactions failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := groupReactions(reactions, userID)
	return httputil.Success(c, result)
}

// ListReactionUsers handles GET /api/v1/channels/:channelID/messages/:messageID/reactions/:emoji.
func (h *ReactionHandler) ListReactionUsers(c fiber.Ctx) error {
	_, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	messageID, err := uuid.Parse(c.Params("messageID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidMessageID, "Invalid message ID format")
	}

	emojiID, emojiUnicode, err := parseEmojiParam(c.Params("emoji"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	}

	reactions, err := h.reactions.ListByEmoji(c, messageID, emojiID, emojiUnicode)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "reaction").Msg("list reaction users failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	users := make([]models.ReactionUser, len(reactions))
	for i := range reactions {
		users[i] = models.ReactionUser{
			UserID:   reactions[i].UserID.String(),
			Username: reactions[i].Username,
		}
	}
	return httputil.Success(c, users)
}

// parseEmojiParam parses the :emoji route parameter. Custom emoji use the "custom:{uuid}" format. Everything else is
// treated as a unicode emoji string.
func parseEmojiParam(param string) (*uuid.UUID, *string, error) {
	if param == "" {
		return nil, nil, errors.New("emoji parameter is required")
	}

	if strings.HasPrefix(param, "custom:") {
		raw := strings.TrimPrefix(param, "custom:")
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, nil, errors.New("invalid custom emoji ID format")
		}
		return &id, nil, nil
	}

	return nil, &param, nil
}

// groupReactions aggregates individual reactions into grouped summaries with counts and the Me flag.
func groupReactions(reactions []reaction.Reaction, currentUser uuid.UUID) []models.ReactionSummary {
	type key struct {
		emojiID      string
		emojiUnicode string
	}

	order := make([]key, 0)
	counts := make(map[key]*models.ReactionSummary)

	for _, r := range reactions {
		k := key{}
		if r.EmojiID != nil {
			k.emojiID = r.EmojiID.String()
		}
		if r.EmojiUnicode != nil {
			k.emojiUnicode = *r.EmojiUnicode
		}

		summary, exists := counts[k]
		if !exists {
			summary = &models.ReactionSummary{}
			if r.EmojiID != nil {
				s := r.EmojiID.String()
				summary.EmojiID = &s
			}
			if r.EmojiUnicode != nil {
				summary.EmojiUnicode = r.EmojiUnicode
			}
			counts[k] = summary
			order = append(order, k)
		}
		summary.Count++
		if r.UserID == currentUser {
			summary.Me = true
		}
	}

	result := make([]models.ReactionSummary, len(order))
	for i, k := range order {
		result[i] = *counts[k]
	}
	return result
}

// mapReactionError converts reaction-layer errors to appropriate HTTP responses.
func (h *ReactionHandler) mapReactionError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, reaction.ErrAlreadyReacted):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.AlreadyReacted, "You have already reacted with this emoji")
	case errors.Is(err, reaction.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.ReactionNotFound, "Reaction not found")
	case errors.Is(err, message.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownMessage, "Message not found")
	default:
		h.log.Error().Err(err).Str("handler", "reaction").Msg("unhandled reaction service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
