package api

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/search"
)

// SearchHandler serves message search endpoints.
type SearchHandler struct {
	service *search.Service
	log     zerolog.Logger
}

// NewSearchHandler creates a new search handler.
func NewSearchHandler(service *search.Service, logger zerolog.Logger) *SearchHandler {
	return &SearchHandler{service: service, log: logger}
}

// SearchMessages handles GET /api/v1/search/messages. It returns messages matching the query, scoped to channels the
// authenticated user has permission to view.
func (h *SearchHandler) SearchMessages(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "The q parameter is required")
	}

	channelID := c.Query("channel_id")
	if channelID != "" {
		if _, err := uuid.Parse(channelID); err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid channel_id format")
		}
	}

	authorID := c.Query("author_id")
	if authorID != "" {
		if _, err := uuid.Parse(authorID); err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid author_id format")
		}
	}

	var before int64
	if raw := c.Query("before"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid before parameter")
		}
		before = v
	}

	var after int64
	if raw := c.Query("after"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid after parameter")
		}
		after = v
	}

	page, _ := strconv.Atoi(c.Query("page"))
	perPage, _ := strconv.Atoi(c.Query("limit"))
	page, perPage = search.ClampPagination(page, perPage)

	result, err := h.service.Search(c, userID, query, search.Options{
		ChannelID: channelID,
		AuthorID:  authorID,
		Before:    before,
		After:     after,
		Page:      page,
		PerPage:   perPage,
	})
	if err != nil {
		return h.mapSearchError(c, err)
	}
	return httputil.Success(c, result)
}

func (h *SearchHandler) mapSearchError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, search.ErrEmptyQuery):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, search.ErrInvalidFilter):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, search.ErrSearchUnavailable):
		return httputil.Fail(c, fiber.StatusServiceUnavailable, apierrors.SearchUnavailable, err.Error())
	default:
		h.log.Error().Err(err).Str("handler", "search").Msg("unhandled search service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
