package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/server"
)

// ServerHandler serves server configuration endpoints.
type ServerHandler struct {
	servers server.Repository
	log     zerolog.Logger
}

// NewServerHandler creates a new server handler.
func NewServerHandler(servers server.Repository, logger zerolog.Logger) *ServerHandler {
	return &ServerHandler{servers: servers, log: logger}
}

// Get handles GET /api/v1/server.
func (h *ServerHandler) Get(c fiber.Ctx) error {
	cfg, err := h.servers.Get(c)
	if err != nil {
		return h.mapServerError(c, err)
	}

	return httputil.Success(c, cfg.ToModel())
}

// GetPublicInfo handles GET /api/v1/server/info (unauthenticated).
func (h *ServerHandler) GetPublicInfo(c fiber.Ctx) error {
	cfg, err := h.servers.Get(c)
	if err != nil {
		return h.mapServerError(c, err)
	}

	return httputil.Success(c, models.PublicServerInfo{
		Name:        cfg.Name,
		Description: cfg.Description,
		IconKey:     cfg.IconKey,
	})
}

// Update handles PATCH /api/v1/server.
func (h *ServerHandler) Update(c fiber.Ctx) error {
	var body models.UpdateServerConfigRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	if err := server.ValidateName(body.Name); err != nil {
		return h.mapServerError(c, err)
	}
	if err := server.ValidateDescription(body.Description); err != nil {
		return h.mapServerError(c, err)
	}

	cfg, err := h.servers.Update(c, server.UpdateParams{
		Name:        body.Name,
		Description: body.Description,
		IconKey:     body.IconKey,
		BannerKey:   body.BannerKey,
	})
	if err != nil {
		return h.mapServerError(c, err)
	}

	return httputil.Success(c, cfg.ToModel())
}

// mapServerError converts server-layer errors to appropriate HTTP responses.
func (h *ServerHandler) mapServerError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, server.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.NotFound, "Server config not found")
	case errors.Is(err, server.ErrNameLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, server.ErrDescriptionLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	default:
		h.log.Error().Err(err).Str("handler", "server").Msg("unhandled server service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
