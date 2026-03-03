package httputil

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	apierrors "github.com/uncord-chat/uncord-protocol/errors"
)

// SuccessResponse wraps successful API responses.
type SuccessResponse struct {
	Data any `json:"data"`
}

// ErrorBody holds structured error details.
type ErrorBody struct {
	Code    apierrors.Code `json:"code"`
	Message string         `json:"message"`
}

// ErrorResponse wraps failed API responses.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// Success sends a 200 JSON response with the given data.
func Success(c fiber.Ctx, data any) error {
	return c.JSON(SuccessResponse{Data: data})
}

// SuccessStatus sends a JSON response with a custom status code.
func SuccessStatus(c fiber.Ctx, status int, data any) error {
	return c.Status(status).JSON(SuccessResponse{Data: data})
}

// Fail sends a JSON error response with the given status, code, and message.
func Fail(c fiber.Ctx, status int, code apierrors.Code, message string) error {
	return c.Status(status).JSON(ErrorResponse{
		Error: ErrorBody{
			Code:    code,
			Message: message,
		},
	})
}

// ParseUUIDParam parses a UUID route parameter. On success it returns the parsed UUID and true. On failure it writes a
// 400 JSON error response with the given code and returns (uuid.Nil, false). The error message is derived from the
// parameter name (e.g. "channelID" produces "Invalid channel ID format"). Callers should return nil when ok is false
// because the response has already been committed.
func ParseUUIDParam(c fiber.Ctx, param string, code apierrors.Code) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Params(param))
	if err != nil {
		label := strings.TrimSuffix(param, "ID") + " ID"
		_ = Fail(c, fiber.StatusBadRequest, code, "Invalid "+label+" format")
		return uuid.Nil, false
	}
	return id, true
}

// UserID extracts the authenticated user's ID from the request context. If the value is absent (indicating the
// RequireAuth middleware has not run), it returns a *fiber.Error that the global error handler renders as a 401.
func UserID(c fiber.Ctx) (uuid.UUID, error) {
	id, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return uuid.Nil, fiber.NewError(fiber.StatusUnauthorized, "Missing user identity")
	}
	return id, nil
}
