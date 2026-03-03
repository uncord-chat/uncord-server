package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// AuthHandler serves authentication endpoints.
type AuthHandler struct {
	auth *auth.Service
	cfg  *config.Config
	log  zerolog.Logger
}

// NewAuthHandler creates a new authentication handler.
func NewAuthHandler(svc *auth.Service, cfg *config.Config, logger zerolog.Logger) *AuthHandler {
	return &AuthHandler{auth: svc, cfg: cfg, log: logger}
}

// toAuthResponse maps a service Result to the protocol response type.
func toAuthResponse(result *auth.Result) models.AuthResponse {
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

	auth.SetAuthCookies(c, h.cfg, result.AccessToken, result.RefreshToken)
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

	if result.MFARequired {
		return httputil.Success(c, models.MFARequiredResponse{
			MFARequired: true,
			Ticket:      result.Ticket,
		})
	}

	auth.SetAuthCookies(c, h.cfg, result.Auth.AccessToken, result.Auth.RefreshToken)
	return httputil.Success(c, toAuthResponse(result.Auth))
}

// MFAVerify handles POST /api/v1/auth/mfa/verify.
func (h *AuthHandler) MFAVerify(c fiber.Ctx) error {
	var body models.MFAVerifyRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}
	if body.Ticket == "" || body.Code == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "ticket and code are required")
	}

	result, err := h.auth.VerifyMFA(c, body.Ticket, body.Code)
	if err != nil {
		return h.mapAuthError(c, err)
	}

	auth.SetAuthCookies(c, h.cfg, result.AccessToken, result.RefreshToken)
	return httputil.Success(c, toAuthResponse(result))
}

// Refresh handles POST /api/v1/auth/refresh.
func (h *AuthHandler) Refresh(c fiber.Ctx) error {
	var body models.RefreshRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	refreshToken := body.RefreshToken
	if refreshToken == "" {
		refreshToken = c.Cookies(auth.RefreshCookieName(h.cfg))
	}
	if refreshToken == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "refresh_token is required")
	}

	tokens, err := h.auth.Refresh(c, refreshToken)
	if err != nil {
		return h.mapAuthError(c, err)
	}

	auth.SetAuthCookies(c, h.cfg, tokens.AccessToken, tokens.RefreshToken)
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

	result, err := h.auth.VerifyEmail(c, body.Token)
	if err != nil {
		return h.mapAuthError(c, err)
	}

	auth.SetAuthCookies(c, h.cfg, result.AccessToken, result.RefreshToken)
	return httputil.Success(c, toAuthResponse(result))
}

// VerifyPassword handles POST /api/v1/auth/verify-password.
func (h *AuthHandler) VerifyPassword(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	var body models.VerifyPasswordRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}
	if body.Password == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "password is required")
	}

	if err := h.auth.VerifyUserPassword(c, userID, body.Password); err != nil {
		return h.mapAuthError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ResendVerification handles POST /api/v1/auth/resend-verification.
func (h *AuthHandler) ResendVerification(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	if err := h.auth.ResendVerification(c, userID); err != nil {
		return h.mapAuthError(c, err)
	}

	return httputil.Success(c, models.MessageResponse{
		Message: "Verification email sent",
	})
}

// Logout handles POST /api/v1/auth/logout. It revokes all refresh tokens for the authenticated user and clears auth
// cookies from the response.
func (h *AuthHandler) Logout(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	if err := auth.RevokeAllRefreshTokens(c, h.auth.Redis(), userID); err != nil {
		h.log.Error().Err(err).Stringer("user_id", userID).Msg("Failed to revoke refresh tokens on logout")
	}

	auth.ClearAuthCookies(c, h.cfg)
	return c.SendStatus(fiber.StatusNoContent)
}

// GatewayTicket handles POST /api/v1/auth/gateway-ticket. It creates a single-use ticket that web clients present in
// the WebSocket Identify frame instead of a JWT access token.
func (h *AuthHandler) GatewayTicket(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	ticket, err := auth.CreateGatewayTicket(c, h.auth.Redis(), userID)
	if err != nil {
		h.log.Error().Err(err).Stringer("user_id", userID).Msg("Failed to create gateway ticket")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	return httputil.Success(c, models.GatewayTicketResponse{Ticket: ticket})
}

// mapAuthError converts auth-layer errors to appropriate HTTP responses.
func (h *AuthHandler) mapAuthError(c fiber.Ctx, err error) error {
	return mapAuthServiceError(c, err, h.log, "auth")
}

// mapAuthServiceError maps auth service sentinel errors to HTTP responses. All handlers that call auth.Service methods
// delegate to this function so that error-to-status mappings are defined in a single place.
func mapAuthServiceError(c fiber.Ctx, err error, log zerolog.Logger, handler string) error {
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
	case errors.Is(err, auth.ErrInvalidMFACode):
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.InvalidMFACode, err.Error())
	case errors.Is(err, auth.ErrMFANotEnabled):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.MFANotEnabled, err.Error())
	case errors.Is(err, auth.ErrMFAAlreadyEnabled):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.MFAAlreadyEnabled, err.Error())
	case errors.Is(err, auth.ErrMFASetupLocked):
		return httputil.Fail(c, fiber.StatusTooManyRequests, apierrors.RateLimited, err.Error())
	case errors.Is(err, auth.ErrMFANotConfigured):
		return httputil.Fail(c, fiber.StatusServiceUnavailable, apierrors.ServiceUnavailable, "MFA is not configured on this server")
	case errors.Is(err, auth.ErrRefreshTokenReused):
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.TokenReused, "Refresh token has already been used")
	case errors.Is(err, auth.ErrRefreshTokenNotFound):
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.InvalidToken, err.Error())
	case errors.Is(err, auth.ErrInvalidToken):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidToken, err.Error())
	case errors.Is(err, auth.ErrServerOwner):
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.ServerOwner, err.Error())
	case errors.Is(err, auth.ErrAccountTombstoned):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.AccountDeleted, "This email or username is not available")
	case errors.Is(err, auth.ErrEmailAlreadyVerified):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.EmailAlreadyVerified, err.Error())
	case errors.Is(err, auth.ErrVerificationCooldown):
		return httputil.Fail(c, fiber.StatusTooManyRequests, apierrors.RateLimited, err.Error())

	default:
		log.Error().Err(err).Str("handler", handler).Msg("unhandled auth service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
