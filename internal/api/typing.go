package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/presence"
)

// TypingHandler serves the typing indicator endpoint.
type TypingHandler struct {
	presence *presence.Store
	gateway  *gateway.Publisher
	log      zerolog.Logger
}

// NewTypingHandler creates a new typing handler.
func NewTypingHandler(presenceStore *presence.Store, gw *gateway.Publisher, logger zerolog.Logger) *TypingHandler {
	return &TypingHandler{
		presence: presenceStore,
		gateway:  gw,
		log:      logger,
	}
}

// StartTyping handles POST /api/v1/channels/:channelID/typing. It records a typing indicator for the authenticated
// user, deduplicating rapid calls via a short-lived Valkey key. When the key is newly created, a TYPING_START dispatch
// event is published to the gateway.
func (h *TypingHandler) StartTyping(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid channel ID")
	}

	created, err := h.presence.SetTyping(c, channelID, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "typing").Msg("set typing failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if created && h.gateway != nil {
		data := models.TypingStartData{
			ChannelID: channelID.String(),
			UserID:    userID.String(),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		if pErr := h.gateway.Publish(c, events.TypingStart, data); pErr != nil {
			h.log.Warn().Err(pErr).Msg("Failed to publish typing start")
		}
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// StopTyping handles DELETE /api/v1/channels/:channelID/typing. It clears the typing indicator for the authenticated
// user and publishes a TYPING_STOP dispatch event when the key existed.
func (h *TypingHandler) StopTyping(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid channel ID")
	}

	existed, err := h.presence.ClearTyping(c, channelID, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "typing").Msg("clear typing failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if existed && h.gateway != nil {
		data := models.TypingStopData{
			ChannelID: channelID.String(),
			UserID:    userID.String(),
		}
		if pErr := h.gateway.Publish(c, events.TypingStop, data); pErr != nil {
			h.log.Warn().Err(pErr).Msg("Failed to publish typing stop")
		}
	}

	return c.SendStatus(fiber.StatusNoContent)
}
