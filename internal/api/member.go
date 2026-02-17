package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/role"
)

// MemberHandler serves member and ban endpoints.
type MemberHandler struct {
	members  member.Repository
	roles    role.Repository
	perms    permission.Store
	resolver *permission.Resolver
	pub      *permission.Publisher
	gateway  *gateway.Publisher
	log      zerolog.Logger
}

// NewMemberHandler creates a new member handler.
func NewMemberHandler(members member.Repository, roles role.Repository, perms permission.Store, resolver *permission.Resolver, pub *permission.Publisher, gw *gateway.Publisher, logger zerolog.Logger) *MemberHandler {
	return &MemberHandler{members: members, roles: roles, perms: perms, resolver: resolver, pub: pub, gateway: gw, log: logger}
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
		result[i] = members[i].ToModel()
	}
	return httputil.Success(c, result)
}

// ListChannelMembers handles GET /api/v1/channels/:channelID/members. It returns only members who have the ViewChannels
// permission on the specified channel, with cursor-based pagination.
func (h *MemberHandler) ListChannelMembers(c fiber.Ctx) error {
	channelID, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid channel ID format")
	}

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

	// Fetch members in batches and filter by ViewChannels permission. The batch size is double the requested limit to
	// minimise round trips when most members have the permission. Permission checks are batched per iteration via
	// FilterUsersPermitted to avoid N+1 individual cache lookups.
	const batchMultiplier = 2
	batchSize := limit * batchMultiplier

	var result []models.Member
	cursor := after

	for len(result) < limit {
		batch, err := h.members.List(c, cursor, batchSize)
		if err != nil {
			h.log.Error().Err(err).Str("handler", "member").Msg("list channel members failed")
			return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		}

		userIDs := make([]uuid.UUID, len(batch))
		for i := range batch {
			userIDs[i] = batch[i].UserID
		}

		permitted, err := h.resolver.FilterUsersPermitted(c, userIDs, channelID, permissions.ViewChannels)
		if err != nil {
			h.log.Error().Err(err).Str("handler", "member").Msg("permission check failed")
			return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		}

		for i := range batch {
			if permitted[i] {
				result = append(result, batch[i].ToModel())
				if len(result) >= limit {
					break
				}
			}
		}

		if len(batch) < batchSize {
			break
		}
		cursor = &batch[len(batch)-1].UserID
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
	return httputil.Success(c, m.ToModel())
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

	result := updated.ToModel()
	h.publishMemberUpdate(result, userID.String())
	return httputil.Success(c, result)
}

// Leave handles DELETE /api/v1/server/members/@me.
func (h *MemberHandler) Leave(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	if err := h.checkNotOwner(c, userID); err != nil {
		return h.mapGuardError(c, err)
	}

	if err := h.members.Delete(c, userID); err != nil {
		return h.mapMemberError(c, err)
	}

	h.publishMemberRemove(userID.String())
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
	return httputil.Success(c, m.ToModel())
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

	if err := h.checkHierarchy(c, userID, targetID); err != nil {
		return h.mapGuardError(c, err)
	}

	updated, err := h.members.UpdateNickname(c, targetID, body.Nickname)
	if err != nil {
		return h.mapMemberError(c, err)
	}

	result := updated.ToModel()
	h.publishMemberUpdate(result, targetID.String())
	return httputil.Success(c, result)
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

	if err := h.checkNotOwner(c, targetID); err != nil {
		return h.mapGuardError(c, err)
	}
	if err := h.checkHierarchy(c, userID, targetID); err != nil {
		return h.mapGuardError(c, err)
	}

	if err := h.members.Delete(c, targetID); err != nil {
		return h.mapMemberError(c, err)
	}

	h.publishMemberRemove(targetID.String())
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

	if err := h.checkNotOwner(c, targetID); err != nil {
		return h.mapGuardError(c, err)
	}
	if err := h.checkHierarchy(c, userID, targetID); err != nil {
		return h.mapGuardError(c, err)
	}

	updated, err := h.members.SetTimeout(c, targetID, until)
	if err != nil {
		return h.mapMemberError(c, err)
	}

	result := updated.ToModel()
	h.publishMemberUpdate(result, targetID.String())
	h.invalidateUser(c, targetID)
	return httputil.Success(c, result)
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

	result := updated.ToModel()
	h.publishMemberUpdate(result, targetID.String())
	h.invalidateUser(c, targetID)
	return httputil.Success(c, result)
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

	if err := h.checkNotOwner(c, targetID); err != nil {
		return h.mapGuardError(c, err)
	}
	if err := h.checkHierarchy(c, userID, targetID); err != nil {
		return h.mapGuardError(c, err)
	}

	if err := h.members.Ban(c, targetID, userID, body.Reason, expiresAt); err != nil {
		return h.mapMemberError(c, err)
	}

	h.publishMemberRemove(targetID.String())
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

	bans, err := h.members.ListBans(c, after, limit)
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

	result := updated.ToModel()
	h.publishMemberUpdate(result, targetID.String())
	return httputil.Success(c, result)
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

	if h.gateway != nil {
		if updated, err := h.members.GetByUserID(c, targetID); err == nil {
			h.publishMemberUpdate(updated.ToModel(), targetID.String())
		}
	}

	h.invalidateUser(c, targetID)
	return c.SendStatus(fiber.StatusNoContent)
}

// Sentinel errors for member guard checks. These are mapped to HTTP responses by mapGuardError.
var (
	errOutranked     = errors.New("target has equal or higher role")
	errTargetIsOwner = errors.New("target is server owner")
)

// checkHierarchy verifies that the caller outranks the target member. Returns errOutranked if the target has an equal
// or higher role position, or a wrapped error if the database query fails.
func (h *MemberHandler) checkHierarchy(c fiber.Ctx, callerID, targetID uuid.UUID) error {
	callerPos, err := h.roles.HighestPosition(c, callerID)
	if err != nil {
		return fmt.Errorf("get caller role position: %w", err)
	}

	targetPos, err := h.roles.HighestPosition(c, targetID)
	if err != nil {
		return fmt.Errorf("get target role position: %w", err)
	}

	if targetPos <= callerPos {
		return errOutranked
	}
	return nil
}

// checkNotOwner verifies that the target user is not the server owner. Returns errTargetIsOwner if the target is the
// owner, or a wrapped error if the database query fails.
func (h *MemberHandler) checkNotOwner(c fiber.Ctx, userID uuid.UUID) error {
	isOwner, err := h.perms.IsOwner(c, userID)
	if err != nil {
		return fmt.Errorf("check ownership: %w", err)
	}
	if isOwner {
		return errTargetIsOwner
	}
	return nil
}

// mapGuardError translates hierarchy and ownership sentinel errors into structured HTTP responses.
func (h *MemberHandler) mapGuardError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, errOutranked):
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.RoleHierarchy,
			"Cannot perform this action on a member with an equal or higher role")
	case errors.Is(err, errTargetIsOwner):
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.ServerOwner,
			"The server owner cannot be targeted by this action")
	default:
		h.log.Error().Err(err).Str("handler", "member").Msg("guard check failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}

// publishMemberUpdate fires a best-effort MEMBER_UPDATE gateway event. Uses context.Background because Fiber recycles
// the request context after the handler returns.
func (h *MemberHandler) publishMemberUpdate(m models.Member, userID string) {
	if h.gateway != nil {
		go func() {
			if err := h.gateway.Publish(context.Background(), events.MemberUpdate, m); err != nil {
				h.log.Warn().Err(err).Str("user_id", userID).Msg("Gateway publish failed")
			}
		}()
	}
}

// publishMemberRemove fires a best-effort MEMBER_REMOVE gateway event. Uses context.Background because Fiber recycles
// the request context after the handler returns.
func (h *MemberHandler) publishMemberRemove(userID string) {
	if h.gateway != nil {
		go func() {
			if err := h.gateway.Publish(context.Background(), events.MemberRemove, models.MemberRemoveData{UserID: userID}); err != nil {
				h.log.Warn().Err(err).Str("user_id", userID).Msg("Gateway publish failed")
			}
		}()
	}
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
