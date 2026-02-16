package api

import (
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/role"
)

// MemberHandler serves member and ban endpoints.
type MemberHandler struct {
	members member.Repository
	roles   role.Repository
	perms   permission.Store
	pub     *permission.Publisher
	log     zerolog.Logger
}

// NewMemberHandler creates a new member handler.
func NewMemberHandler(members member.Repository, roles role.Repository, perms permission.Store, pub *permission.Publisher, logger zerolog.Logger) *MemberHandler {
	return &MemberHandler{members: members, roles: roles, perms: perms, pub: pub, log: logger}
}

// ListMembers handles GET /api/v1/server/members.
func (h *MemberHandler) ListMembers(c fiber.Ctx) error {
	var after *uuid.UUID
	if raw := c.Query("after"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid after parameter")
		}
		after = &id
	}

	rawLimit, _ := strconv.Atoi(c.Query("limit"))
	limit := member.ClampLimit(rawLimit)

	members, err := h.members.List(c, after, limit)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "member").Msg("list members failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Member, len(members))
	for i := range members {
		result[i] = toMemberModel(&members[i])
	}
	return httputil.Success(c, result)
}

// GetSelf handles GET /api/v1/server/members/@me. Unlike other member endpoints, this includes pending members so they
// can check their own onboarding status.
func (h *MemberHandler) GetSelf(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	m, err := h.members.GetByUserIDAnyStatus(c, userID)
	if err != nil {
		return h.mapMemberError(c, err)
	}
	return httputil.Success(c, toMemberModel(m))
}

// UpdateSelf handles PATCH /api/v1/server/members/@me.
func (h *MemberHandler) UpdateSelf(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	var body models.UpdateMemberRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	if err := member.ValidateNickname(body.Nickname); err != nil {
		return h.mapMemberError(c, err)
	}

	updated, err := h.members.UpdateNickname(c, userID, body.Nickname)
	if err != nil {
		return h.mapMemberError(c, err)
	}
	return httputil.Success(c, toMemberModel(updated))
}

// Leave handles DELETE /api/v1/server/members/@me.
func (h *MemberHandler) Leave(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	if h.checkNotOwner(c, userID) {
		return nil
	}

	if err := h.members.Delete(c, userID); err != nil {
		return h.mapMemberError(c, err)
	}

	h.invalidateUser(c, userID)
	return c.SendStatus(fiber.StatusNoContent)
}

// GetMember handles GET /api/v1/server/members/:userID.
func (h *MemberHandler) GetMember(c fiber.Ctx) error {
	targetID, err := uuid.Parse(c.Params("userID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid user ID format")
	}

	m, err := h.members.GetByUserID(c, targetID)
	if err != nil {
		return h.mapMemberError(c, err)
	}
	return httputil.Success(c, toMemberModel(m))
}

// UpdateMember handles PATCH /api/v1/server/members/:userID.
func (h *MemberHandler) UpdateMember(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	targetID, err := uuid.Parse(c.Params("userID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid user ID format")
	}

	var body models.UpdateMemberRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	if err := member.ValidateNickname(body.Nickname); err != nil {
		return h.mapMemberError(c, err)
	}

	if h.checkHierarchy(c, userID, targetID) {
		return nil
	}

	updated, err := h.members.UpdateNickname(c, targetID, body.Nickname)
	if err != nil {
		return h.mapMemberError(c, err)
	}
	return httputil.Success(c, toMemberModel(updated))
}

// KickMember handles DELETE /api/v1/server/members/:userID.
func (h *MemberHandler) KickMember(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	targetID, err := uuid.Parse(c.Params("userID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid user ID format")
	}

	if h.checkNotOwner(c, targetID) {
		return nil
	}
	if h.checkHierarchy(c, userID, targetID) {
		return nil
	}

	if err := h.members.Delete(c, targetID); err != nil {
		return h.mapMemberError(c, err)
	}

	h.invalidateUser(c, targetID)
	return c.SendStatus(fiber.StatusNoContent)
}

// SetTimeout handles PUT /api/v1/server/members/:userID/timeout.
func (h *MemberHandler) SetTimeout(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	targetID, err := uuid.Parse(c.Params("userID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid user ID format")
	}

	var body models.TimeoutMemberRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	until, err := time.Parse(time.RFC3339, body.Until)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid timestamp format, expected RFC3339")
	}
	if !until.After(time.Now()) {
		return h.mapMemberError(c, member.ErrTimeoutInPast)
	}

	if h.checkNotOwner(c, targetID) {
		return nil
	}
	if h.checkHierarchy(c, userID, targetID) {
		return nil
	}

	updated, err := h.members.SetTimeout(c, targetID, until)
	if err != nil {
		return h.mapMemberError(c, err)
	}

	h.invalidateUser(c, targetID)
	return httputil.Success(c, toMemberModel(updated))
}

// ClearTimeout handles DELETE /api/v1/server/members/:userID/timeout.
func (h *MemberHandler) ClearTimeout(c fiber.Ctx) error {
	targetID, err := uuid.Parse(c.Params("userID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid user ID format")
	}

	updated, err := h.members.ClearTimeout(c, targetID)
	if err != nil {
		return h.mapMemberError(c, err)
	}

	h.invalidateUser(c, targetID)
	return httputil.Success(c, toMemberModel(updated))
}

// BanMember handles PUT /api/v1/server/bans/:userID.
func (h *MemberHandler) BanMember(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	targetID, err := uuid.Parse(c.Params("userID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid user ID format")
	}

	var body models.BanMemberRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	var expiresAt *time.Time
	if body.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *body.ExpiresAt)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid expires_at format, expected RFC3339")
		}
		expiresAt = &t
	}

	if h.checkNotOwner(c, targetID) {
		return nil
	}
	if h.checkHierarchy(c, userID, targetID) {
		return nil
	}

	if err := h.members.Ban(c, targetID, userID, body.Reason, expiresAt); err != nil {
		return h.mapMemberError(c, err)
	}

	h.invalidateUser(c, targetID)
	return c.SendStatus(fiber.StatusNoContent)
}

// UnbanMember handles DELETE /api/v1/server/bans/:userID.
func (h *MemberHandler) UnbanMember(c fiber.Ctx) error {
	targetID, err := uuid.Parse(c.Params("userID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid user ID format")
	}

	if err := h.members.Unban(c, targetID); err != nil {
		return h.mapMemberError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListBans handles GET /api/v1/server/bans.
func (h *MemberHandler) ListBans(c fiber.Ctx) error {
	bans, err := h.members.ListBans(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "member").Msg("list bans failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Ban, len(bans))
	for i := range bans {
		result[i] = toBanModel(&bans[i])
	}
	return httputil.Success(c, result)
}

// AssignRole handles PUT /api/v1/server/members/:userID/roles/:roleID.
func (h *MemberHandler) AssignRole(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	targetID, err := uuid.Parse(c.Params("userID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid user ID format")
	}

	roleID, err := uuid.Parse(c.Params("roleID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid role ID format")
	}

	r, err := h.roles.GetByID(c, roleID)
	if err != nil {
		if errors.Is(err, role.ErrNotFound) {
			return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownRole, "Role not found")
		}
		h.log.Error().Err(err).Str("handler", "member").Msg("failed to look up role")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if r.IsEveryone {
		return h.mapMemberError(c, member.ErrEveryoneRole)
	}

	// The assigned role's position must be below the caller's highest position (higher number = lower rank).
	callerPos, err := h.roles.HighestPosition(c, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "member").Msg("failed to get caller highest position")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if r.Position <= callerPos {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.RoleHierarchy,
			"Cannot assign a role at or above your highest role")
	}

	// Verify the target member exists.
	if _, err := h.members.GetByUserID(c, targetID); err != nil {
		return h.mapMemberError(c, err)
	}

	if err := h.members.AssignRole(c, targetID, roleID); err != nil {
		return h.mapMemberError(c, err)
	}

	h.invalidateUser(c, targetID)

	updated, err := h.members.GetByUserID(c, targetID)
	if err != nil {
		return h.mapMemberError(c, err)
	}
	return httputil.Success(c, toMemberModel(updated))
}

// RemoveRole handles DELETE /api/v1/server/members/:userID/roles/:roleID.
func (h *MemberHandler) RemoveRole(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	targetID, err := uuid.Parse(c.Params("userID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid user ID format")
	}

	roleID, err := uuid.Parse(c.Params("roleID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid role ID format")
	}

	r, err := h.roles.GetByID(c, roleID)
	if err != nil {
		if errors.Is(err, role.ErrNotFound) {
			return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownRole, "Role not found")
		}
		h.log.Error().Err(err).Str("handler", "member").Msg("failed to look up role")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if r.IsEveryone {
		return h.mapMemberError(c, member.ErrEveryoneRole)
	}

	callerPos, err := h.roles.HighestPosition(c, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "member").Msg("failed to get caller highest position")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if r.Position <= callerPos {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.RoleHierarchy,
			"Cannot remove a role at or above your highest role")
	}

	if err := h.members.RemoveRole(c, targetID, roleID); err != nil {
		return h.mapMemberError(c, err)
	}

	h.invalidateUser(c, targetID)
	return c.SendStatus(fiber.StatusNoContent)
}

// checkHierarchy verifies that the caller outranks the target member. Returns true if the handler should stop because
// an error response was already written.
func (h *MemberHandler) checkHierarchy(c fiber.Ctx, callerID, targetID uuid.UUID) bool {
	callerPos, err := h.roles.HighestPosition(c, callerID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "member").Msg("failed to get caller highest position")
		_ = httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		return true
	}

	targetPos, err := h.roles.HighestPosition(c, targetID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "member").Msg("failed to get target highest position")
		_ = httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		return true
	}

	if targetPos <= callerPos {
		_ = httputil.Fail(c, fiber.StatusForbidden, apierrors.RoleHierarchy,
			"Cannot perform this action on a member with an equal or higher role")
		return true
	}
	return false
}

// checkNotOwner verifies that the target user is not the server owner. Returns true if the handler should stop because
// an error response was already written.
func (h *MemberHandler) checkNotOwner(c fiber.Ctx, userID uuid.UUID) bool {
	isOwner, err := h.perms.IsOwner(c, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "member").Msg("failed to check ownership")
		_ = httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		return true
	}
	if isOwner {
		_ = httputil.Fail(c, fiber.StatusForbidden, apierrors.ServerOwner,
			"The server owner cannot be targeted by this action")
		return true
	}
	return false
}

// invalidateUser publishes a cache invalidation for the given user. Failures are logged but not surfaced to the caller
// because a stale cache entry will expire naturally.
func (h *MemberHandler) invalidateUser(c fiber.Ctx, userID uuid.UUID) {
	if h.pub != nil {
		if err := h.pub.InvalidateUser(c, userID); err != nil {
			h.log.Warn().Err(err).Msg("failed to invalidate permission cache for user")
		}
	}
}

// toMemberModel converts the internal member type to the protocol response type.
func toMemberModel(m *member.MemberWithProfile) models.Member {
	roleIDs := make([]string, len(m.RoleIDs))
	for i, id := range m.RoleIDs {
		roleIDs[i] = id.String()
	}

	result := models.Member{
		User: models.MemberUser{
			ID:          m.UserID.String(),
			Username:    m.Username,
			DisplayName: m.DisplayName,
			AvatarKey:   m.AvatarKey,
		},
		Nickname: m.Nickname,
		JoinedAt: m.JoinedAt.Format(time.RFC3339),
		Roles:    roleIDs,
		Status:   m.Status,
	}
	if m.TimeoutUntil != nil {
		s := m.TimeoutUntil.Format(time.RFC3339)
		result.TimeoutUntil = &s
	}
	return result
}

// toBanModel converts the internal ban record to the protocol response type.
func toBanModel(b *member.BanRecord) models.Ban {
	result := models.Ban{
		User: models.MemberUser{
			ID:          b.UserID.String(),
			Username:    b.Username,
			DisplayName: b.DisplayName,
			AvatarKey:   b.AvatarKey,
		},
		Reason:    b.Reason,
		CreatedAt: b.CreatedAt.Format(time.RFC3339),
	}
	if b.BannedBy != nil {
		s := b.BannedBy.String()
		result.BannedBy = &s
	}
	if b.ExpiresAt != nil {
		s := b.ExpiresAt.Format(time.RFC3339)
		result.ExpiresAt = &s
	}
	return result
}

// mapMemberError converts member-layer errors to appropriate HTTP responses.
func (h *MemberHandler) mapMemberError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, member.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownMember, "Member not found")
	case errors.Is(err, member.ErrBanNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownBan, "Ban not found")
	case errors.Is(err, member.ErrNicknameLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, member.ErrAlreadyMember):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.AlreadyMember, err.Error())
	case errors.Is(err, member.ErrAlreadyBanned):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.AlreadyExists, err.Error())
	case errors.Is(err, member.ErrEveryoneRole):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, member.ErrTimeoutInPast):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	default:
		h.log.Error().Err(err).Str("handler", "member").Msg("unhandled member service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
