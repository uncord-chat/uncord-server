package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// UserHandler serves user profile endpoints.
type UserHandler struct {
	users user.Repository
	auth  *auth.Service
	log   zerolog.Logger
}

// NewUserHandler creates a new user handler.
func NewUserHandler(users user.Repository, authSvc *auth.Service, logger zerolog.Logger) *UserHandler {
	return &UserHandler{users: users, auth: authSvc, log: logger}
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

	return httputil.Success(c, u.ToModel())
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

	user.NormalizePronouns(body.Pronouns)
	if err := user.ValidatePronouns(body.Pronouns); err != nil {
		return h.mapUserError(c, err)
	}

	user.NormalizeAbout(body.About)
	if err := user.ValidateAbout(body.About); err != nil {
		return h.mapUserError(c, err)
	}

	if err := user.ValidateThemeColour(body.ThemeColourPrimary); err != nil {
		return h.mapUserError(c, err)
	}
	if err := user.ValidateThemeColour(body.ThemeColourSecondary); err != nil {
		return h.mapUserError(c, err)
	}

	u, err := h.users.Update(c, userID, user.UpdateParams{
		DisplayName:          body.DisplayName,
		AvatarKey:            body.AvatarKey,
		Pronouns:             body.Pronouns,
		BannerKey:            body.BannerKey,
		About:                body.About,
		ThemeColourPrimary:   body.ThemeColourPrimary,
		ThemeColourSecondary: body.ThemeColourSecondary,
	})
	if err != nil {
		return h.mapUserError(c, err)
	}

	return httputil.Success(c, u.ToModel())
}

// DeleteMe handles DELETE /api/v1/users/@me.
func (h *UserHandler) DeleteMe(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	var body models.DeleteAccountRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}
	if body.Password == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "password is required")
	}

	if err := h.auth.DeleteAccount(c, userID, body.Password); err != nil {
		return mapAuthServiceError(c, err, h.log, "user")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// mapUserError converts user-layer errors to appropriate HTTP responses.
func (h *UserHandler) mapUserError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, user.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownUser, "User not found")
	case errors.Is(err, user.ErrDisplayNameLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, user.ErrPronounsLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, user.ErrAboutLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, user.ErrThemeColourRange):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	default:
		h.log.Error().Err(err).Str("handler", "user").Msg("unhandled user service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
