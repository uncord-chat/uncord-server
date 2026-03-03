package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/message"
)

// mapMessageError converts message-layer errors to appropriate HTTP responses. It is shared across all handlers that
// create or modify messages (channels, DMs, threads).
func mapMessageError(c fiber.Ctx, err error, log zerolog.Logger) error {
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
		log.Error().Err(err).Str("handler", "message").Msg("unhandled message service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
