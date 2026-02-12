package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// AuthHandler serves authentication endpoints.
type AuthHandler struct {
	Auth *auth.Service
}

// registerRequest is the JSON body for POST /api/v1/auth/register.
type registerRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginRequest is the JSON body for POST /api/v1/auth/login.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// refreshRequest is the JSON body for POST /api/v1/auth/refresh.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// verifyEmailRequest is the JSON body for POST /api/v1/auth/verify-email.
type verifyEmailRequest struct {
	Token string `json:"token"`
}

// authResultResponse builds the JSON payload for Register and Login responses.
func authResultResponse(result *auth.AuthResult) fiber.Map {
	return fiber.Map{
		"user": fiber.Map{
			"id":             result.User.ID,
			"email":          result.User.Email,
			"username":       result.User.Username,
			"email_verified": result.User.EmailVerified,
		},
		"access_token":  result.AccessToken,
		"refresh_token": result.RefreshToken,
	}
}

// Register handles POST /api/v1/auth/register.
func (h *AuthHandler) Register(c *fiber.Ctx) error {
	var body registerRequest
	if err := c.BodyParser(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, "INVALID_BODY", "Invalid request body")
	}

	result, err := h.Auth.Register(c.Context(), auth.RegisterRequest{
		Email:    body.Email,
		Username: body.Username,
		Password: body.Password,
	})
	if err != nil {
		return mapAuthError(c, err)
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, authResultResponse(result))
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var body loginRequest
	if err := c.BodyParser(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, "INVALID_BODY", "Invalid request body")
	}

	result, err := h.Auth.Login(c.Context(), auth.LoginRequest{
		Email:    body.Email,
		Password: body.Password,
		IP:       c.IP(),
	})
	if err != nil {
		return mapAuthError(c, err)
	}

	return httputil.Success(c, authResultResponse(result))
}

// Refresh handles POST /api/v1/auth/refresh.
func (h *AuthHandler) Refresh(c *fiber.Ctx) error {
	var body refreshRequest
	if err := c.BodyParser(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, "INVALID_BODY", "Invalid request body")
	}
	if body.RefreshToken == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, "INVALID_BODY", "refresh_token is required")
	}

	tokens, err := h.Auth.Refresh(c.Context(), body.RefreshToken)
	if err != nil {
		return mapAuthError(c, err)
	}

	return httputil.Success(c, fiber.Map{
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
	})
}

// VerifyEmail handles POST /api/v1/auth/verify-email.
func (h *AuthHandler) VerifyEmail(c *fiber.Ctx) error {
	var body verifyEmailRequest
	if err := c.BodyParser(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, "INVALID_BODY", "Invalid request body")
	}
	if body.Token == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, "INVALID_BODY", "token is required")
	}

	if err := h.Auth.VerifyEmail(c.Context(), body.Token); err != nil {
		return mapAuthError(c, err)
	}

	return httputil.Success(c, fiber.Map{
		"message": "Email verified successfully",
	})
}

// mapAuthError converts auth-layer errors to appropriate HTTP responses.
func mapAuthError(c *fiber.Ctx, err error) error {
	switch {
	// Validation errors
	case errors.Is(err, auth.ErrInvalidEmail):
		return httputil.Fail(c, fiber.StatusBadRequest, "INVALID_EMAIL", err.Error())
	case errors.Is(err, auth.ErrUsernameLength),
		errors.Is(err, auth.ErrUsernameInvalidChars):
		return httputil.Fail(c, fiber.StatusBadRequest, "INVALID_USERNAME", err.Error())
	case errors.Is(err, auth.ErrPasswordTooShort),
		errors.Is(err, auth.ErrPasswordTooLong):
		return httputil.Fail(c, fiber.StatusBadRequest, "INVALID_PASSWORD", err.Error())

	// Business logic errors
	case errors.Is(err, auth.ErrDisposableEmail):
		return httputil.Fail(c, fiber.StatusBadRequest, "DISPOSABLE_EMAIL", err.Error())
	case errors.Is(err, auth.ErrEmailAlreadyTaken):
		return httputil.Fail(c, fiber.StatusConflict, "ALREADY_EXISTS", err.Error())
	case errors.Is(err, auth.ErrInvalidCredentials):
		return httputil.Fail(c, fiber.StatusUnauthorized, "INVALID_CREDENTIALS", err.Error())
	case errors.Is(err, auth.ErrMFARequired):
		return httputil.Fail(c, fiber.StatusForbidden, "MFA_REQUIRED", err.Error())
	case errors.Is(err, auth.ErrRefreshTokenReused):
		return httputil.Fail(c, fiber.StatusUnauthorized, "TOKEN_REUSED", "Refresh token has already been used")
	case errors.Is(err, auth.ErrInvalidToken):
		return httputil.Fail(c, fiber.StatusBadRequest, "INVALID_TOKEN", err.Error())

	default:
		return httputil.Fail(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "An internal error occurred")
	}
}
