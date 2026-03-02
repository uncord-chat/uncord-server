package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/audit"
	"github.com/uncord-chat/uncord-server/internal/emoji"
	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/media"
)

// EmojiHandler serves custom emoji endpoints.
type EmojiHandler struct {
	emoji       emoji.Repository
	storage     media.StorageProvider
	gateway     *gateway.Publisher
	maxSize     int64
	maxDim      int
	limit       int
	auditLogger *audit.Logger
	log         zerolog.Logger
}

// NewEmojiHandler creates a new emoji handler.
func NewEmojiHandler(
	emojiRepo emoji.Repository,
	storage media.StorageProvider,
	gw *gateway.Publisher,
	maxSize int64,
	maxDim int,
	limit int,
	auditLogger *audit.Logger,
	logger zerolog.Logger,
) *EmojiHandler {
	return &EmojiHandler{
		emoji:       emojiRepo,
		storage:     storage,
		gateway:     gw,
		maxSize:     maxSize,
		maxDim:      maxDim,
		limit:       limit,
		auditLogger: auditLogger,
		log:         logger,
	}
}

// ListEmoji handles GET /api/v1/server/emoji.
func (h *EmojiHandler) ListEmoji(c fiber.Ctx) error {
	list, err := h.emoji.List(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "emoji").Msg("list emoji failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Emoji, len(list))
	for i := range list {
		result[i] = list[i].ToModel(h.storage.URL(list[i].StorageKey))
	}
	return httputil.Success(c, result)
}

// CreateEmoji handles POST /api/v1/server/emoji.
func (h *EmojiHandler) CreateEmoji(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	name := c.FormValue("name")
	if err := emoji.ValidateName(name); err != nil {
		return h.mapEmojiError(c, err)
	}

	count, err := h.emoji.Count(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "emoji").Msg("count emoji failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if count >= h.limit {
		return h.mapEmojiError(c, emoji.ErrLimitReached)
	}

	fh, err := c.FormFile("file")
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Missing file field in multipart form")
	}

	if fh.Size > h.maxSize {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.PayloadTooLarge,
			fmt.Sprintf("File size exceeds the maximum of %d KB", h.maxSize/1024))
	}

	contentType := detectContentType(fh.Header.Get("Content-Type"), fh.Filename)
	if !media.IsAvatarContentType(contentType) {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.UnsupportedContentType,
			"This file type is not allowed for emoji")
	}

	animated := contentType == "image/gif"

	f, err := fh.Open()
	if err != nil {
		h.log.Error().Err(err).Msg("Failed to open uploaded emoji file")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	defer func() { _ = f.Close() }()

	buf, err := media.ResizeImage(f, h.maxDim, h.maxDim)
	if err != nil {
		h.log.Error().Err(err).Msg("Failed to process uploaded emoji image")
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Could not process image")
	}

	storageKey := fmt.Sprintf("emoji/%s.webp", uuid.New().String())

	if err := h.storage.Put(c.Context(), storageKey, buf); err != nil {
		h.log.Error().Err(err).Msg("Failed to write emoji to storage")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	created, err := h.emoji.Create(c, emoji.CreateParams{
		Name:       name,
		Animated:   animated,
		StorageKey: storageKey,
		UploaderID: userID,
	})
	if err != nil {
		_ = h.storage.Delete(c.Context(), storageKey)
		return h.mapEmojiError(c, err)
	}

	if h.auditLogger != nil {
		go h.auditLogger.Record(context.Background(), audit.Entry{
			ActorID: userID, Action: audit.EmojiCreate,
			TargetType: audit.Ptr("emoji"), TargetID: audit.UUIDPtr(created.ID),
		})
	}

	h.publishEmojiUpdate()

	return httputil.SuccessStatus(c, fiber.StatusCreated, created.ToModel(h.storage.URL(created.StorageKey)))
}

// UpdateEmoji handles PATCH /api/v1/server/emoji/:emojiID.
func (h *EmojiHandler) UpdateEmoji(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	emojiID, err := uuid.Parse(c.Params("emojiID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid emoji ID format")
	}

	var body models.UpdateEmojiRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	if err := emoji.ValidateName(body.Name); err != nil {
		return h.mapEmojiError(c, err)
	}

	updated, err := h.emoji.UpdateName(c, emojiID, body.Name)
	if err != nil {
		return h.mapEmojiError(c, err)
	}

	if h.auditLogger != nil {
		go h.auditLogger.Record(context.Background(), audit.Entry{
			ActorID: userID, Action: audit.EmojiUpdate,
			TargetType: audit.Ptr("emoji"), TargetID: audit.UUIDPtr(emojiID),
		})
	}

	h.publishEmojiUpdate()

	return httputil.Success(c, updated.ToModel(h.storage.URL(updated.StorageKey)))
}

// DeleteEmoji handles DELETE /api/v1/server/emoji/:emojiID.
func (h *EmojiHandler) DeleteEmoji(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	emojiID, err := uuid.Parse(c.Params("emojiID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid emoji ID format")
	}

	existing, err := h.emoji.GetByID(c, emojiID)
	if err != nil {
		return h.mapEmojiError(c, err)
	}

	if err := h.emoji.Delete(c, emojiID); err != nil {
		return h.mapEmojiError(c, err)
	}

	if h.auditLogger != nil {
		go h.auditLogger.Record(context.Background(), audit.Entry{
			ActorID: userID, Action: audit.EmojiDelete,
			TargetType: audit.Ptr("emoji"), TargetID: audit.UUIDPtr(emojiID),
		})
	}

	if delErr := h.storage.Delete(c.Context(), existing.StorageKey); delErr != nil {
		h.log.Warn().Err(delErr).Str("key", existing.StorageKey).Msg("Failed to delete emoji file")
	}

	h.publishEmojiUpdate()

	return c.SendStatus(fiber.StatusNoContent)
}

// publishEmojiUpdate sends an EMOJI_UPDATE gateway event with the full current emoji list. The DB query runs in its own
// goroutine to avoid blocking the HTTP response; the resulting event is published via the bounded worker pool.
func (h *EmojiHandler) publishEmojiUpdate() {
	if h.gateway == nil {
		return
	}
	go func() {
		list, err := h.emoji.List(context.Background())
		if err != nil {
			h.log.Warn().Err(err).Msg("Failed to load emoji list for gateway event")
			return
		}
		emojiModels := make([]models.Emoji, len(list))
		for i := range list {
			emojiModels[i] = list[i].ToModel(h.storage.URL(list[i].StorageKey))
		}
		h.gateway.Enqueue(events.EmojiUpdate, models.EmojiUpdateData{Emoji: emojiModels})
	}()
}

// mapEmojiError converts emoji-layer errors to appropriate HTTP responses.
func (h *EmojiHandler) mapEmojiError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, emoji.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownEmoji, "Custom emoji not found")
	case errors.Is(err, emoji.ErrNameTaken):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.EmojiNameTaken, "An emoji with that name already exists")
	case errors.Is(err, emoji.ErrLimitReached):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.MaxEmojiReached, "Maximum number of custom emoji reached")
	case errors.Is(err, emoji.ErrInvalidName):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidEmojiName, err.Error())
	default:
		h.log.Error().Err(err).Str("handler", "emoji").Msg("unhandled emoji service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
