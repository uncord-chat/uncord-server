package api

import (
	"encoding/base64"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/settingsync"
)

// SettingSyncHandler serves the encrypted settings sync endpoints.
type SettingSyncHandler struct {
	repo settingsync.Repository
	log  zerolog.Logger
}

// NewSettingSyncHandler creates a new settings sync handler.
func NewSettingSyncHandler(repo settingsync.Repository, logger zerolog.Logger) *SettingSyncHandler {
	return &SettingSyncHandler{repo: repo, log: logger}
}

// Get handles GET /api/v1/users/@me/synced-settings.
func (h *SettingSyncHandler) Get(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	blob, err := h.repo.Get(c, userID)
	if err != nil {
		return h.mapSettingSyncError(c, err)
	}

	return httputil.Success(c, models.SyncedSettingsBlob{
		EncryptedBlob: base64.StdEncoding.EncodeToString(blob.EncryptedBlob),
		Salt:          base64.StdEncoding.EncodeToString(blob.Salt),
		Nonce:         base64.StdEncoding.EncodeToString(blob.Nonce),
		BlobVersion:   blob.BlobVersion,
		UpdatedAt:     blob.UpdatedAt.Format(time.RFC3339),
	})
}

// Put handles PUT /api/v1/users/@me/synced-settings.
func (h *SettingSyncHandler) Put(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	var body models.PutSyncedSettingsRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	encryptedBlob, err := base64.StdEncoding.DecodeString(body.EncryptedBlob)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid base64 in encrypted_blob")
	}

	salt, err := base64.StdEncoding.DecodeString(body.Salt)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid base64 in salt")
	}

	nonce, err := base64.StdEncoding.DecodeString(body.Nonce)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid base64 in nonce")
	}

	params := settingsync.UpsertParams{
		EncryptedBlob: encryptedBlob,
		Salt:          salt,
		Nonce:         nonce,
		BlobVersion:   body.BlobVersion,
	}

	if err := settingsync.ValidateUpsertParams(params); err != nil {
		return h.mapSettingSyncError(c, err)
	}

	blob, err := h.repo.Upsert(c, userID, params)
	if err != nil {
		return h.mapSettingSyncError(c, err)
	}

	return httputil.Success(c, models.PutSyncedSettingsResponse{
		BlobVersion: blob.BlobVersion,
		UpdatedAt:   blob.UpdatedAt.Format(time.RFC3339),
	})
}

// Delete handles DELETE /api/v1/users/@me/synced-settings.
func (h *SettingSyncHandler) Delete(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	if err := h.repo.Delete(c, userID); err != nil {
		return h.mapSettingSyncError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// mapSettingSyncError converts settings sync layer errors to appropriate HTTP responses.
func (h *SettingSyncHandler) mapSettingSyncError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, settingsync.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.SyncedSettingsNotFound, "No synced settings stored")
	case errors.Is(err, settingsync.ErrVersionConflict):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.SyncedSettingsVersionConflict, err.Error())
	case errors.Is(err, settingsync.ErrSaltLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, settingsync.ErrNonceLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, settingsync.ErrBlobTooLarge):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.PayloadTooLarge, err.Error())
	case errors.Is(err, settingsync.ErrBlobEmpty):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, settingsync.ErrVersionInvalid):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	default:
		h.log.Error().Err(err).Str("handler", "settingsync").Msg("unhandled settings sync error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
