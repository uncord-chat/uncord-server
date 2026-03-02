package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/readstate"
)

// ReadStateHandler serves channel read state endpoints.
type ReadStateHandler struct {
	readStates readstate.Repository
	gateway    *gateway.Publisher
	log        zerolog.Logger
}

// NewReadStateHandler creates a new read state handler.
func NewReadStateHandler(readStates readstate.Repository, gw *gateway.Publisher, logger zerolog.Logger) *ReadStateHandler {
	return &ReadStateHandler{
		readStates: readStates,
		gateway:    gw,
		log:        logger,
	}
}

// Ack handles POST /api/v1/channels/:channelID/ack. It advances the user's read position in the channel to the
// specified message and dispatches a MESSAGE_ACK event to the user's connected sessions.
func (h *ReadStateHandler) Ack(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	var body models.AckRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	messageID, err := uuid.Parse(body.MessageID)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidMessageID, "Invalid message ID format")
	}

	rs, err := h.readStates.Ack(c, userID, channelID, messageID)
	if err != nil {
		return h.mapReadStateError(c, err)
	}

	if h.gateway != nil {
		h.gateway.EnqueueTargeted(events.MessageAck, &models.MessageAckData{
			ChannelID: channelID.String(),
			MessageID: messageID.String(),
		}, []uuid.UUID{userID})
	}

	return httputil.Success(c, rs.ToModel())
}

// mapReadStateError converts readstate-layer errors to appropriate HTTP responses.
func (h *ReadStateHandler) mapReadStateError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, readstate.ErrMessageNotInChannel):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidMessageID, "Message does not exist in this channel")
	default:
		h.log.Error().Err(err).Str("handler", "readstate").Msg("unhandled read state error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
