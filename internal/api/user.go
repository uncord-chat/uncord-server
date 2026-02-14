package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// UserHandler serves user profile endpoints.
type UserHandler struct {
	users user.Repository
	log   zerolog.Logger
}

// NewUserHandler creates a new user handler.
func NewUserHandler(users user.Repository, logger zerolog.Logger) *UserHandler {
	return &UserHandler{users: users, log: logger}
}

// GetMe handles GET /api/v1/users/@me.
func (h *UserHandler) GetMe(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	u, err := h.users.GetByID(c, userID)
	if err != nil {
		return h.mapUserError(c, err)
	}

	return httputil.Success(c, toUserModel(u))
}

// UpdateMe handles PATCH /api/v1/users/@me.
func (h *UserHandler) UpdateMe(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	var body models.UpdateUserRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	user.NormalizeDisplayName(body.DisplayName)
	if err := user.ValidateDisplayName(body.DisplayName); err != nil {
		return h.mapUserError(c, err)
	}

	u, err := h.users.Update(c, userID, user.UpdateParams{
		DisplayName: body.DisplayName,
		AvatarKey:   body.AvatarKey,
	})
	if err != nil {
		return h.mapUserError(c, err)
	}

	return httputil.Success(c, toUserModel(u))
}

// toUserModel converts the internal user struct to the protocol response type.
func toUserModel(u *user.User) models.User {
	return models.User{
		ID:            u.ID.String(),
		Email:         u.Email,
		Username:      u.Username,
		DisplayName:   u.DisplayName,
		AvatarKey:     u.AvatarKey,
		MFAEnabled:    u.MFAEnabled,
		EmailVerified: u.EmailVerified,
	}
}

// mapUserError converts user-layer errors to appropriate HTTP responses.
func (h *UserHandler) mapUserError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, user.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.NotFound, "User not found")
	case errors.Is(err, user.ErrDisplayNameLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	default:
		h.log.Error().Err(err).Str("handler", "user").Msg("unhandled user service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
