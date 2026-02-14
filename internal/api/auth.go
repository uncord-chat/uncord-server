package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// AuthHandler serves authentication endpoints.
type AuthHandler struct {
	auth *auth.Service
	log  zerolog.Logger
}

// NewAuthHandler creates a new authentication handler.
func NewAuthHandler(svc *auth.Service, logger zerolog.Logger) *AuthHandler {
	return &AuthHandler{auth: svc, log: logger}
}

// toAuthResponse maps a service AuthResult to the protocol response type.
func toAuthResponse(result *auth.AuthResult) models.AuthResponse {
	return models.AuthResponse{
		User:         result.User,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	}
}

// Register handles POST /api/v1/auth/register.
func (h *AuthHandler) Register(c fiber.Ctx) error {
	var body models.RegisterRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	result, err := h.auth.Register(c, auth.RegisterRequest{
		Email:    body.Email,
		Username: body.Username,
		Password: body.Password,
	})
	if err != nil {
		return h.mapAuthError(c, err)
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, toAuthResponse(result))
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(c fiber.Ctx) error {
	var body models.LoginRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	result, err := h.auth.Login(c, auth.LoginRequest{
		Email:    body.Email,
		Password: body.Password,
		IP:       c.IP(),
	})
	if err != nil {
		return h.mapAuthError(c, err)
	}

	return httputil.Success(c, toAuthResponse(result))
}

// Refresh handles POST /api/v1/auth/refresh.
func (h *AuthHandler) Refresh(c fiber.Ctx) error {
	var body models.RefreshRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}
	if body.RefreshToken == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "refresh_token is required")
	}

	tokens, err := h.auth.Refresh(c, body.RefreshToken)
	if err != nil {
		return h.mapAuthError(c, err)
	}

	return httputil.Success(c, models.TokenPairResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	})
}

// VerifyEmail handles POST /api/v1/auth/verify-email.
func (h *AuthHandler) VerifyEmail(c fiber.Ctx) error {
	var body models.VerifyEmailRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}
	if body.Token == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "token is required")
	}

	if err := h.auth.VerifyEmail(c, body.Token); err != nil {
		return h.mapAuthError(c, err)
	}

	return httputil.Success(c, models.MessageResponse{
		Message: "Email verified successfully",
	})
}

// mapAuthError converts auth-layer errors to appropriate HTTP responses.
func (h *AuthHandler) mapAuthError(c fiber.Ctx, err error) error {
	switch {
	// Validation errors
	case errors.Is(err, auth.ErrInvalidEmail):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidEmail, err.Error())
	case errors.Is(err, auth.ErrUsernameLength),
		errors.Is(err, auth.ErrUsernameInvalidChars):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidUsername, err.Error())
	case errors.Is(err, auth.ErrPasswordTooShort),
		errors.Is(err, auth.ErrPasswordTooLong):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidPassword, err.Error())

	// Business logic errors
	case errors.Is(err, auth.ErrDisposableEmail):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.DisposableEmail, err.Error())
	case errors.Is(err, auth.ErrEmailAlreadyTaken):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidEmail, "Unable to register with the provided email")
	case errors.Is(err, auth.ErrInvalidCredentials):
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.InvalidCredentials, err.Error())
	case errors.Is(err, auth.ErrMFARequired):
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.MFARequired, err.Error())
	case errors.Is(err, auth.ErrRefreshTokenReused):
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.TokenReused, "Refresh token has already been used")
	case errors.Is(err, auth.ErrInvalidToken):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidToken, err.Error())

	default:
		h.log.Error().Err(err).Str("handler", "auth").Msg("unhandled auth service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
