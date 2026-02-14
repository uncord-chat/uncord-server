package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// MFAHandler serves authenticated MFA management endpoints under /api/v1/users/@me/mfa/.
type MFAHandler struct {
	auth *auth.Service
	log  zerolog.Logger
}

// NewMFAHandler creates a new MFA handler.
func NewMFAHandler(svc *auth.Service, logger zerolog.Logger) *MFAHandler {
	return &MFAHandler{auth: svc, log: logger}
}

// Enable handles POST /api/v1/users/@me/mfa/enable.
func (h *MFAHandler) Enable(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	var body models.MFAEnableRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}
	if body.Password == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "password is required")
	}

	result, err := h.auth.BeginMFASetup(c, userID, body.Password)
	if err != nil {
		return h.mapMFAError(c, err)
	}

	return httputil.Success(c, models.MFASetupResponse{
		Secret: result.Secret,
		URI:    result.URI,
	})
}

// Confirm handles POST /api/v1/users/@me/mfa/confirm.
func (h *MFAHandler) Confirm(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	var body models.MFAConfirmRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}
	if body.Code == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "code is required")
	}

	codes, err := h.auth.ConfirmMFASetup(c, userID, body.Code)
	if err != nil {
		return h.mapMFAError(c, err)
	}

	return httputil.Success(c, models.MFAConfirmResponse{
		RecoveryCodes: codes,
	})
}

// Disable handles POST /api/v1/users/@me/mfa/disable.
func (h *MFAHandler) Disable(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	var body models.MFADisableRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}
	if body.Password == "" || body.Code == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "password and code are required")
	}

	if err := h.auth.DisableMFA(c, userID, body.Password, body.Code); err != nil {
		return h.mapMFAError(c, err)
	}

	return httputil.Success(c, models.MessageResponse{
		Message: "MFA has been disabled",
	})
}

// RegenerateCodes handles POST /api/v1/users/@me/mfa/recovery-codes.
func (h *MFAHandler) RegenerateCodes(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	var body models.MFARegenerateCodesRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}
	if body.Password == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "password is required")
	}

	codes, err := h.auth.RegenerateRecoveryCodes(c, userID, body.Password)
	if err != nil {
		return h.mapMFAError(c, err)
	}

	return httputil.Success(c, models.MFARegenerateCodesResponse{
		RecoveryCodes: codes,
	})
}

// mapMFAError converts MFA-layer errors to appropriate HTTP responses.
func (h *MFAHandler) mapMFAError(c fiber.Ctx, err error) error {
	return mapAuthServiceError(c, err, h.log, "mfa")
}
