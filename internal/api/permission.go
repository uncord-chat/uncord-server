package api

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/permission"
)

// PermissionHandler serves permission override endpoints.
type PermissionHandler struct {
	overrides permission.OverrideStore
	resolver  *permission.Resolver
	perms     *permission.Publisher
	log       zerolog.Logger
}

// NewPermissionHandler creates a new permission handler.
func NewPermissionHandler(overrides permission.OverrideStore, resolver *permission.Resolver, perms *permission.Publisher, logger zerolog.Logger) *PermissionHandler {
	return &PermissionHandler{overrides: overrides, resolver: resolver, perms: perms, log: logger}
}

// SetOverride handles PUT /api/v1/channels/:channelID/overrides/:targetID.
func (h *PermissionHandler) SetOverride(c fiber.Ctx) error {
	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	targetID, err := uuid.Parse(c.Params("targetID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid target ID format")
	}

	var body models.SetOverrideRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	principalType, err := parsePrincipalType(body.Type)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	}

	if err := validateOverrideBits(body.Allow, body.Deny); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	}

	row, err := h.overrides.Set(c, permission.TargetChannel, channelID, principalType, targetID,
		permissions.Permission(body.Allow), permissions.Permission(body.Deny))
	if err != nil {
		return h.mapOverrideError(c, err)
	}

	if h.perms != nil {
		if err := h.perms.InvalidateChannel(c, channelID); err != nil {
			h.log.Warn().Err(err).Msg("failed to invalidate permission cache after override upsert")
		}
	}

	return httputil.Success(c, toOverrideModel(row))
}

// DeleteOverride handles DELETE /api/v1/channels/:channelID/overrides/:targetID.
func (h *PermissionHandler) DeleteOverride(c fiber.Ctx) error {
	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	targetID, err := uuid.Parse(c.Params("targetID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid target ID format")
	}

	principalType, err := parsePrincipalType(c.Query("type"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	}

	if err := h.overrides.Delete(c, permission.TargetChannel, channelID, principalType, targetID); err != nil {
		return h.mapOverrideError(c, err)
	}

	if h.perms != nil {
		if err := h.perms.InvalidateChannel(c, channelID); err != nil {
			h.log.Warn().Err(err).Msg("failed to invalidate permission cache after override deletion")
		}
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GetMyPermissions handles GET /api/v1/channels/:channelID/permissions/@me.
func (h *PermissionHandler) GetMyPermissions(c fiber.Ctx) error {
	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	perm, err := h.resolver.Resolve(c, userID, channelID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "permission").Msg("resolve permissions failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	return httputil.Success(c, models.ResolvedPermissions{Permissions: int64(perm)})
}

// toOverrideModel converts an internal OverrideRow to the protocol response type.
func toOverrideModel(row *permission.OverrideRow) models.PermissionOverride {
	return models.PermissionOverride{
		ID:        row.ID.String(),
		Type:      string(row.PrincipalType),
		TargetID:  row.PrincipalID.String(),
		Allow:     int64(row.Allow),
		Deny:      int64(row.Deny),
		CreatedAt: row.CreatedAt.Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.Format(time.RFC3339),
	}
}

// parsePrincipalType validates and converts a string to a PrincipalType.
func parsePrincipalType(s string) (permission.PrincipalType, error) {
	switch s {
	case string(permission.PrincipalRole):
		return permission.PrincipalRole, nil
	case string(permission.PrincipalUser):
		return permission.PrincipalUser, nil
	default:
		return "", errors.New("type must be \"role\" or \"user\"")
	}
}

// validateOverrideBits checks that the allow and deny bitfields contain no bits beyond AllPermissions.
func validateOverrideBits(allow, deny int64) error {
	mask := int64(permissions.AllPermissions)
	if allow & ^mask != 0 {
		return errors.New("allow contains invalid permission bits")
	}
	if deny & ^mask != 0 {
		return errors.New("deny contains invalid permission bits")
	}
	return nil
}

// mapOverrideError converts override-layer errors to appropriate HTTP responses.
func (h *PermissionHandler) mapOverrideError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, permission.ErrOverrideNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownOverride, "Permission override not found")
	default:
		h.log.Error().Err(err).Str("handler", "permission").Msg("unhandled permission override error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
