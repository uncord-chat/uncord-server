package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
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
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	channelID, ok := httputil.ParseUUIDParam(c, "channelID", apierrors.InvalidChannelID)
	if !ok {
		return nil
	}

	created, err := h.presence.SetTyping(c, channelID, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "typing").Msg("set typing failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if created && h.gateway != nil {
		h.gateway.Enqueue(events.TypingStart, models.TypingStartData{
			ChannelID: channelID.String(),
			UserID:    userID.String(),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// StopTyping handles DELETE /api/v1/channels/:channelID/typing. It clears the typing indicator for the authenticated
// user and publishes a TYPING_STOP dispatch event when the key existed.
func (h *TypingHandler) StopTyping(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	channelID, ok := httputil.ParseUUIDParam(c, "channelID", apierrors.InvalidChannelID)
	if !ok {
		return nil
	}

	existed, err := h.presence.ClearTyping(c, channelID, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "typing").Msg("clear typing failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if existed && h.gateway != nil {
		h.gateway.Enqueue(events.TypingStop, models.TypingStopData{
			ChannelID: channelID.String(),
			UserID:    userID.String(),
		})
	}

	return c.SendStatus(fiber.StatusNoContent)
}
