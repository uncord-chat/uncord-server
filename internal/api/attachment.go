package api

import (
	"context"
	"errors"
	"fmt"
	"image"
	// Register standard image decoders for dimension detection.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/attachment"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/media"
)

// AttachmentHandler serves file upload endpoints.
type AttachmentHandler struct {
	attachments  attachment.Repository
	storage      media.StorageProvider
	rdb          *redis.Client
	maxSizeBytes int64
	log          zerolog.Logger
}

// NewAttachmentHandler creates a new attachment handler.
func NewAttachmentHandler(
	attachments attachment.Repository,
	storage media.StorageProvider,
	rdb *redis.Client,
	maxSizeBytes int64,
	logger zerolog.Logger,
) *AttachmentHandler {
	return &AttachmentHandler{
		attachments:  attachments,
		storage:      storage,
		rdb:          rdb,
		maxSizeBytes: maxSizeBytes,
		log:          logger,
	}
}

// Upload handles POST /api/v1/channels/:channelID/attachments.
func (h *AttachmentHandler) Upload(c fiber.Ctx) error {
	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	fh, err := c.FormFile("file")
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Missing file field in multipart form")
	}

	if fh.Size > h.maxSizeBytes {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.PayloadTooLarge,
			fmt.Sprintf("File size exceeds the maximum of %d MB", h.maxSizeBytes/(1024*1024)))
	}

	contentType := detectContentType(fh.Header.Get("Content-Type"), fh.Filename)
	if !media.IsAllowedContentType(contentType) {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.UnsupportedContentType,
			"This file type is not allowed")
	}

	f, err := fh.Open()
	if err != nil {
		h.log.Error().Err(err).Msg("Failed to open uploaded file")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	defer func() { _ = f.Close() }()

	// Detect image dimensions (best-effort).
	var width, height *int
	if media.IsImageContentType(contentType) {
		cfg, _, decErr := image.DecodeConfig(f)
		if decErr == nil {
			w, h := cfg.Width, cfg.Height
			width = &w
			height = &h
		}
		// Reset the reader to the beginning for storage.
		if seeker, ok := f.(interface {
			Seek(int64, int) (int64, error)
		}); ok {
			if _, err := seeker.Seek(0, 0); err != nil {
				h.log.Error().Err(err).Msg("Failed to seek uploaded file")
				return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
			}
		}
	}

	ext := media.ExtensionFromFilename(fh.Filename)
	storageKey := fmt.Sprintf("attachments/%s/%s%s", channelID.String(), uuid.New().String(), ext)

	if err := h.storage.Put(c.Context(), storageKey, f); err != nil {
		h.log.Error().Err(err).Msg("Failed to write file to storage")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	a, err := h.attachments.Create(c.Context(), attachment.CreateParams{
		ChannelID:   channelID,
		UploaderID:  userID,
		Filename:    sanitiseFilename(fh.Filename),
		ContentType: contentType,
		SizeBytes:   fh.Size,
		StorageKey:  storageKey,
		Width:       width,
		Height:      height,
	})
	if err != nil {
		// Best-effort cleanup of the stored file.
		_ = h.storage.Delete(c.Context(), storageKey)
		h.log.Error().Err(err).Msg("Failed to create attachment record")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	// Enqueue thumbnail generation for images (best-effort). Uses context.Background because Fiber recycles the
	// request context after the handler returns.
	if media.IsImageContentType(contentType) && h.rdb != nil {
		go func() {
			job := media.ThumbnailJob{
				AttachmentID: a.ID.String(),
				StorageKey:   storageKey,
				ContentType:  contentType,
			}
			if err := media.EnqueueThumbnail(context.Background(), h.rdb, job); err != nil {
				h.log.Warn().Err(err).Str("attachment_id", a.ID.String()).Msg("Failed to enqueue thumbnail job")
			}
		}()
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, toAttachmentModel(a, h.storage))
}

// toAttachmentModel converts an internal attachment to the protocol response type.
func toAttachmentModel(a *attachment.Attachment, storage media.StorageProvider) models.Attachment {
	result := models.Attachment{
		ID:          a.ID.String(),
		Filename:    a.Filename,
		URL:         storage.URL(a.StorageKey),
		Size:        a.SizeBytes,
		ContentType: a.ContentType,
		Width:       a.Width,
		Height:      a.Height,
	}
	if a.ThumbnailKey != nil {
		url := storage.URL(*a.ThumbnailKey)
		result.ThumbnailURL = &url
	}
	return result
}

// sanitiseFilename strips path components and limits the filename to 255 characters.
func sanitiseFilename(name string) string {
	name = filepath.Base(name)
	if utf8.RuneCountInString(name) > 255 {
		runes := []rune(name)
		name = string(runes[:255])
	}
	return name
}

// detectContentType returns the MIME type from the multipart header, falling back to extension-based detection.
func detectContentType(header, filename string) string {
	ct := strings.TrimSpace(header)
	if ct != "" && ct != "application/octet-stream" {
		return ct
	}
	ext := filepath.Ext(filename)
	if ext != "" {
		if mt := mime.TypeByExtension(ext); mt != "" {
			return mt
		}
	}
	return ct
}

// mapAttachmentError converts attachment-layer errors to appropriate HTTP responses.
func mapAttachmentError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, attachment.ErrNotFound):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.UnknownAttachment,
			"One or more attachment IDs are invalid or unavailable")
	case errors.Is(err, media.ErrUnsupportedContentType):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.UnsupportedContentType,
			"This file type is not allowed")
	case errors.Is(err, media.ErrFileTooLarge):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.PayloadTooLarge,
			"File exceeds the maximum upload size")
	default:
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
