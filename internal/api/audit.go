package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/audit"
	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// AuditHandler serves the audit log endpoint.
type AuditHandler struct {
	repo audit.Repository
	log  zerolog.Logger
}

// NewAuditHandler creates a new audit log handler.
func NewAuditHandler(repo audit.Repository, logger zerolog.Logger) *AuditHandler {
	return &AuditHandler{repo: repo, log: logger}
}

// List handles GET /api/v1/server/audit-log.
func (h *AuditHandler) List(c fiber.Ctx) error {
	var params audit.ListParams

	if raw := c.Query("actor_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid actor_id parameter")
		}
		params.ActorID = &id
	}

	if raw := c.Query("action_type"); raw != "" {
		action := audit.ActionType(raw)
		params.ActionType = &action
	}

	if raw := c.Query("target_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid target_id parameter")
		}
		params.TargetID = &id
	}

	if raw := c.Query("before"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid before parameter")
		}
		params.Before = &id
	}

	rawLimit, ok := httputil.ParseIntQuery(c, "limit")
	if !ok {
		return nil
	}
	params.Limit = audit.ClampLimit(rawLimit)

	entries, err := h.repo.List(c, params)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "audit").Msg("list audit log failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.AuditLogEntry, len(entries))
	for i := range entries {
		result[i] = entries[i].ToModel()
	}
	return httputil.Success(c, result)
}
