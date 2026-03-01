package api

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/media"
	"github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// ImageUploadHandler serves avatar, banner, and server icon upload and delete endpoints.
type ImageUploadHandler struct {
	users          user.Repository
	servers        server.Repository
	storage        media.StorageProvider
	maxAvatarBytes int64
	maxAvatarDim   int
	maxBannerW     int
	maxBannerH     int
	log            zerolog.Logger
}

// NewImageUploadHandler creates a new handler for image upload and delete operations.
func NewImageUploadHandler(
	users user.Repository,
	servers server.Repository,
	storage media.StorageProvider,
	maxAvatarBytes int64,
	maxAvatarDim int,
	maxBannerW, maxBannerH int,
	logger zerolog.Logger,
) *ImageUploadHandler {
	return &ImageUploadHandler{
		users:          users,
		servers:        servers,
		storage:        storage,
		maxAvatarBytes: maxAvatarBytes,
		maxAvatarDim:   maxAvatarDim,
		maxBannerW:     maxBannerW,
		maxBannerH:     maxBannerH,
		log:            logger,
	}
}

// UploadUserAvatar handles PUT /api/v1/users/@me/avatar.
func (h *ImageUploadHandler) UploadUserAvatar(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	buf, err := h.readAndResize(c, h.maxAvatarDim, h.maxAvatarDim)
	if err != nil || buf == nil {
		return err
	}

	storageKey := fmt.Sprintf("avatars/%s/%s.webp", userID.String(), uuid.New().String())

	// Read current key for cleanup after successful update.
	current, err := h.users.GetByID(c, userID)
	if err != nil {
		return h.mapUserError(c, err)
	}

	if err := h.storage.Put(c.Context(), storageKey, buf); err != nil {
		h.log.Error().Err(err).Msg("Failed to write avatar to storage")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	updated, err := h.users.SetAvatarKey(c, userID, storageKey)
	if err != nil {
		_ = h.storage.Delete(c.Context(), storageKey)
		return h.mapUserError(c, err)
	}

	if current.AvatarKey != nil {
		if delErr := h.storage.Delete(c.Context(), *current.AvatarKey); delErr != nil {
			h.log.Warn().Err(delErr).Str("key", *current.AvatarKey).Msg("Failed to delete old avatar file")
		}
	}

	return httputil.Success(c, updated.ToModel())
}

// DeleteUserAvatar handles DELETE /api/v1/users/@me/avatar.
func (h *ImageUploadHandler) DeleteUserAvatar(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	current, err := h.users.GetByID(c, userID)
	if err != nil {
		return h.mapUserError(c, err)
	}
	if current.AvatarKey == nil {
		return httputil.Success(c, current.ToModel())
	}

	oldKey := *current.AvatarKey
	updated, err := h.users.ClearAvatarKey(c, userID)
	if err != nil {
		return h.mapUserError(c, err)
	}

	if delErr := h.storage.Delete(c.Context(), oldKey); delErr != nil {
		h.log.Warn().Err(delErr).Str("key", oldKey).Msg("Failed to delete avatar file")
	}

	return httputil.Success(c, updated.ToModel())
}

// UploadUserBanner handles PUT /api/v1/users/@me/banner.
func (h *ImageUploadHandler) UploadUserBanner(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	buf, err := h.readAndResize(c, h.maxBannerW, h.maxBannerH)
	if err != nil || buf == nil {
		return err
	}

	storageKey := fmt.Sprintf("banners/%s/%s.webp", userID.String(), uuid.New().String())

	current, err := h.users.GetByID(c, userID)
	if err != nil {
		return h.mapUserError(c, err)
	}

	if err := h.storage.Put(c.Context(), storageKey, buf); err != nil {
		h.log.Error().Err(err).Msg("Failed to write banner to storage")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	updated, err := h.users.SetBannerKey(c, userID, storageKey)
	if err != nil {
		_ = h.storage.Delete(c.Context(), storageKey)
		return h.mapUserError(c, err)
	}

	if current.BannerKey != nil {
		if delErr := h.storage.Delete(c.Context(), *current.BannerKey); delErr != nil {
			h.log.Warn().Err(delErr).Str("key", *current.BannerKey).Msg("Failed to delete old banner file")
		}
	}

	return httputil.Success(c, updated.ToModel())
}

// DeleteUserBanner handles DELETE /api/v1/users/@me/banner.
func (h *ImageUploadHandler) DeleteUserBanner(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	current, err := h.users.GetByID(c, userID)
	if err != nil {
		return h.mapUserError(c, err)
	}
	if current.BannerKey == nil {
		return httputil.Success(c, current.ToModel())
	}

	oldKey := *current.BannerKey
	updated, err := h.users.ClearBannerKey(c, userID)
	if err != nil {
		return h.mapUserError(c, err)
	}

	if delErr := h.storage.Delete(c.Context(), oldKey); delErr != nil {
		h.log.Warn().Err(delErr).Str("key", oldKey).Msg("Failed to delete banner file")
	}

	return httputil.Success(c, updated.ToModel())
}

// UploadServerIcon handles PUT /api/v1/server/icon.
func (h *ImageUploadHandler) UploadServerIcon(c fiber.Ctx) error {
	buf, err := h.readAndResize(c, h.maxAvatarDim, h.maxAvatarDim)
	if err != nil || buf == nil {
		return err
	}

	storageKey := fmt.Sprintf("icons/server/%s.webp", uuid.New().String())

	current, err := h.servers.Get(c)
	if err != nil {
		return h.mapServerError(c, err)
	}

	if err := h.storage.Put(c.Context(), storageKey, buf); err != nil {
		h.log.Error().Err(err).Msg("Failed to write server icon to storage")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	updated, err := h.servers.SetIconKey(c, storageKey)
	if err != nil {
		_ = h.storage.Delete(c.Context(), storageKey)
		return h.mapServerError(c, err)
	}

	if current.IconKey != nil {
		if delErr := h.storage.Delete(c.Context(), *current.IconKey); delErr != nil {
			h.log.Warn().Err(delErr).Str("key", *current.IconKey).Msg("Failed to delete old server icon file")
		}
	}

	return httputil.Success(c, updated.ToModel())
}

// DeleteServerIcon handles DELETE /api/v1/server/icon.
func (h *ImageUploadHandler) DeleteServerIcon(c fiber.Ctx) error {
	current, err := h.servers.Get(c)
	if err != nil {
		return h.mapServerError(c, err)
	}
	if current.IconKey == nil {
		return httputil.Success(c, current.ToModel())
	}

	oldKey := *current.IconKey
	updated, err := h.servers.ClearIconKey(c)
	if err != nil {
		return h.mapServerError(c, err)
	}

	if delErr := h.storage.Delete(c.Context(), oldKey); delErr != nil {
		h.log.Warn().Err(delErr).Str("key", oldKey).Msg("Failed to delete server icon file")
	}

	return httputil.Success(c, updated.ToModel())
}

// UploadServerBanner handles PUT /api/v1/server/banner.
func (h *ImageUploadHandler) UploadServerBanner(c fiber.Ctx) error {
	buf, err := h.readAndResize(c, h.maxBannerW, h.maxBannerH)
	if err != nil || buf == nil {
		return err
	}

	storageKey := fmt.Sprintf("server-banners/server/%s.webp", uuid.New().String())

	current, err := h.servers.Get(c)
	if err != nil {
		return h.mapServerError(c, err)
	}

	if err := h.storage.Put(c.Context(), storageKey, buf); err != nil {
		h.log.Error().Err(err).Msg("Failed to write server banner to storage")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	updated, err := h.servers.SetBannerKey(c, storageKey)
	if err != nil {
		_ = h.storage.Delete(c.Context(), storageKey)
		return h.mapServerError(c, err)
	}

	if current.BannerKey != nil {
		if delErr := h.storage.Delete(c.Context(), *current.BannerKey); delErr != nil {
			h.log.Warn().Err(delErr).Str("key", *current.BannerKey).Msg("Failed to delete old server banner file")
		}
	}

	return httputil.Success(c, updated.ToModel())
}

// DeleteServerBanner handles DELETE /api/v1/server/banner.
func (h *ImageUploadHandler) DeleteServerBanner(c fiber.Ctx) error {
	current, err := h.servers.Get(c)
	if err != nil {
		return h.mapServerError(c, err)
	}
	if current.BannerKey == nil {
		return httputil.Success(c, current.ToModel())
	}

	oldKey := *current.BannerKey
	updated, err := h.servers.ClearBannerKey(c)
	if err != nil {
		return h.mapServerError(c, err)
	}

	if delErr := h.storage.Delete(c.Context(), oldKey); delErr != nil {
		h.log.Warn().Err(delErr).Str("key", oldKey).Msg("Failed to delete server banner file")
	}

	return httputil.Success(c, updated.ToModel())
}

// readAndResize extracts the multipart file, validates size and content type, decodes and resizes the image, and
// returns the WebP-encoded result. On validation failure it writes the error response directly and returns (nil, nil);
// callers must check for a nil buffer before proceeding.
func (h *ImageUploadHandler) readAndResize(c fiber.Ctx, maxW, maxH int) (*bytes.Buffer, error) {
	fh, err := c.FormFile("file")
	if err != nil {
		return nil, httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Missing file field in multipart form")
	}

	if fh.Size > h.maxAvatarBytes {
		return nil, httputil.Fail(c, fiber.StatusBadRequest, apierrors.PayloadTooLarge,
			fmt.Sprintf("File size exceeds the maximum of %d MB", h.maxAvatarBytes/(1024*1024)))
	}

	contentType := detectContentType(fh.Header.Get("Content-Type"), fh.Filename)
	if !media.IsAvatarContentType(contentType) {
		return nil, httputil.Fail(c, fiber.StatusBadRequest, apierrors.UnsupportedContentType,
			"This file type is not allowed for images")
	}

	f, err := fh.Open()
	if err != nil {
		h.log.Error().Err(err).Msg("Failed to open uploaded file")
		return nil, httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	defer func() { _ = f.Close() }()

	buf, err := media.ResizeImage(f, maxW, maxH)
	if err != nil {
		h.log.Error().Err(err).Msg("Failed to process uploaded image")
		return nil, httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Could not process image")
	}

	return buf, nil
}

func (h *ImageUploadHandler) mapUserError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, user.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownUser, "User not found")
	default:
		h.log.Error().Err(err).Str("handler", "image_upload").Msg("unhandled user error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}

func (h *ImageUploadHandler) mapServerError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, server.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.NotFound, "Server config not found")
	default:
		h.log.Error().Err(err).Str("handler", "image_upload").Msg("unhandled server error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
