package api

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/role"
)

// RoleHandler serves role endpoints.
type RoleHandler struct {
	roles    role.Repository
	perms    *permission.Publisher
	gateway  *gateway.Publisher
	maxRoles int
	log      zerolog.Logger
}

// NewRoleHandler creates a new role handler.
func NewRoleHandler(roles role.Repository, perms *permission.Publisher, gw *gateway.Publisher, maxRoles int, logger zerolog.Logger) *RoleHandler {
	return &RoleHandler{roles: roles, perms: perms, gateway: gw, maxRoles: maxRoles, log: logger}
}

// ListRoles handles GET /api/v1/server/roles.
func (h *RoleHandler) ListRoles(c fiber.Ctx) error {
	roles, err := h.roles.List(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "role").Msg("list roles failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Role, len(roles))
	for i := range roles {
		result[i] = roles[i].ToModel()
	}
	return httputil.Success(c, result)
}

// CreateRole handles POST /api/v1/server/roles.
func (h *RoleHandler) CreateRole(c fiber.Ctx) error {
	var body models.CreateRoleRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	name, err := role.ValidateNameRequired(body.Name)
	if err != nil {
		return h.mapRoleError(c, err)
	}
	if err := role.ValidateColour(body.Colour); err != nil {
		return h.mapRoleError(c, err)
	}
	if err := role.ValidatePermissions(body.Permissions); err != nil {
		return h.mapRoleError(c, err)
	}

	params := role.CreateParams{Name: name}
	if body.Colour != nil {
		params.Colour = *body.Colour
	}
	if body.Permissions != nil {
		params.Permissions = *body.Permissions
	}
	if body.Hoist != nil {
		params.Hoist = *body.Hoist
	}

	created, err := h.roles.Create(c, params, h.maxRoles)
	if err != nil {
		return h.mapRoleError(c, err)
	}

	result := created.ToModel()
	if h.gateway != nil {
		go func() {
			if err := h.gateway.Publish(context.Background(), events.RoleCreate, result); err != nil {
				h.log.Warn().Err(err).Str("role_id", created.ID.String()).Msg("Gateway publish failed")
			}
		}()
	}

	if h.perms != nil {
		if err := h.perms.InvalidateAll(c); err != nil {
			h.log.Warn().Err(err).Msg("failed to invalidate permission cache after role creation")
		}
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, result)
}

// UpdateRole handles PATCH /api/v1/server/roles/:roleID.
func (h *RoleHandler) UpdateRole(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	id, err := uuid.Parse(c.Params("roleID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid role ID format")
	}

	var body models.UpdateRoleRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	if err := role.ValidateName(body.Name); err != nil {
		return h.mapRoleError(c, err)
	}
	if err := role.ValidatePosition(body.Position); err != nil {
		return h.mapRoleError(c, err)
	}
	if err := role.ValidateColour(body.Colour); err != nil {
		return h.mapRoleError(c, err)
	}
	if err := role.ValidatePermissions(body.Permissions); err != nil {
		return h.mapRoleError(c, err)
	}

	// Enforce role hierarchy: the caller cannot modify roles at or above their own highest role.
	target, err := h.roles.GetByID(c, id)
	if err != nil {
		return h.mapRoleError(c, err)
	}

	callerPos, err := h.roles.HighestPosition(c, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "role").Msg("failed to get caller highest position")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if target.Position <= callerPos {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.RoleHierarchy, "Cannot modify roles at or above your highest role")
	}
	if body.Position != nil && *body.Position <= callerPos {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.RoleHierarchy, "Cannot move a role to a position at or above your highest role")
	}

	// Prevent renaming the @everyone role.
	if target.IsEveryone && body.Name != nil {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.ValidationError, "The @everyone role cannot be renamed")
	}

	updated, err := h.roles.Update(c, id, role.UpdateParams{
		Name:        body.Name,
		Colour:      body.Colour,
		Position:    body.Position,
		Permissions: body.Permissions,
		Hoist:       body.Hoist,
	})
	if err != nil {
		return h.mapRoleError(c, err)
	}

	result := updated.ToModel()
	if h.gateway != nil {
		go func() {
			if err := h.gateway.Publish(context.Background(), events.RoleUpdate, result); err != nil {
				h.log.Warn().Err(err).Str("role_id", id.String()).Msg("Gateway publish failed")
			}
		}()
	}

	if h.perms != nil {
		if err := h.perms.InvalidateAll(c); err != nil {
			h.log.Warn().Err(err).Msg("failed to invalidate permission cache after role update")
		}
	}

	return httputil.Success(c, result)
}

// DeleteRole handles DELETE /api/v1/server/roles/:roleID.
func (h *RoleHandler) DeleteRole(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	id, err := uuid.Parse(c.Params("roleID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid role ID format")
	}

	// Enforce role hierarchy: the caller cannot delete roles at or above their own highest role.
	target, err := h.roles.GetByID(c, id)
	if err != nil {
		return h.mapRoleError(c, err)
	}

	callerPos, err := h.roles.HighestPosition(c, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "role").Msg("failed to get caller highest position")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if target.Position <= callerPos {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.RoleHierarchy, "Cannot delete roles at or above your highest role")
	}

	if err := h.roles.Delete(c, id); err != nil {
		return h.mapRoleError(c, err)
	}

	if h.gateway != nil {
		go func() {
			if err := h.gateway.Publish(context.Background(), events.RoleDelete, models.RoleDeleteData{ID: id.String()}); err != nil {
				h.log.Warn().Err(err).Str("role_id", id.String()).Msg("Gateway publish failed")
			}
		}()
	}

	if h.perms != nil {
		if err := h.perms.InvalidateAll(c); err != nil {
			h.log.Warn().Err(err).Msg("failed to invalidate permission cache after role deletion")
		}
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// mapRoleError converts role-layer errors to appropriate HTTP responses.
func (h *RoleHandler) mapRoleError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, role.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownRole, "Role not found")
	case errors.Is(err, role.ErrNameLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, role.ErrInvalidPosition):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, role.ErrInvalidPermissions):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, role.ErrInvalidColour):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, role.ErrAlreadyExists):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.AlreadyExists, err.Error())
	case errors.Is(err, role.ErrMaxRolesReached):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.MaxRolesReached, err.Error())
	case errors.Is(err, role.ErrEveryoneImmutable):
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.ValidationError, err.Error())
	default:
		h.log.Error().Err(err).Str("handler", "role").Msg("unhandled role service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
