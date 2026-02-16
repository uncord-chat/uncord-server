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
	"github.com/uncord-chat/uncord-server/internal/invite"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/onboarding"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// InviteHandler serves invite endpoints.
type InviteHandler struct {
	invites    invite.Repository
	onboarding onboarding.Repository
	members    member.Repository
	users      user.Repository
	log        zerolog.Logger
}

// NewInviteHandler creates a new invite handler.
func NewInviteHandler(invites invite.Repository, onboardingRepo onboarding.Repository, members member.Repository, users user.Repository, logger zerolog.Logger) *InviteHandler {
	return &InviteHandler{invites: invites, onboarding: onboardingRepo, members: members, users: users, log: logger}
}

// CreateInvite handles POST /api/v1/server/invites.
func (h *InviteHandler) CreateInvite(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	var body models.CreateInviteRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	channelID, err := uuid.Parse(body.ChannelID)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid channel ID format")
	}

	if err := invite.ValidateMaxUses(body.MaxUses); err != nil {
		return h.mapInviteError(c, err)
	}
	if err := invite.ValidateMaxAge(body.MaxAgeSeconds); err != nil {
		return h.mapInviteError(c, err)
	}

	inv, err := h.invites.Create(c, userID, invite.CreateParams{
		ChannelID:     channelID,
		MaxUses:       body.MaxUses,
		MaxAgeSeconds: body.MaxAgeSeconds,
	})
	if err != nil {
		return h.mapInviteError(c, err)
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, toInviteModel(inv))
}

// ListInvites handles GET /api/v1/server/invites.
func (h *InviteHandler) ListInvites(c fiber.Ctx) error {
	var after *uuid.UUID
	if raw := c.Query("after"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid after parameter")
		}
		after = &id
	}

	rawLimit, _ := strconv.Atoi(c.Query("limit"))
	limit := invite.ClampLimit(rawLimit)

	invites, err := h.invites.List(c, after, limit)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "invite").Msg("list invites failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Invite, len(invites))
	for i := range invites {
		result[i] = toInviteModel(&invites[i])
	}
	return httputil.Success(c, result)
}

// DeleteInvite handles DELETE /api/v1/invites/:code.
func (h *InviteHandler) DeleteInvite(c fiber.Ctx) error {
	code := c.Params("code")
	if err := h.invites.Delete(c, code); err != nil {
		return h.mapInviteError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// JoinViaInvite handles POST /api/v1/invites/:code/join.
func (h *InviteHandler) JoinViaInvite(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	// Check ban before consuming the invite.
	banned, err := h.members.IsBanned(c, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "invite").Msg("ban check failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if banned {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.Banned, "You are banned from this server")
	}

	code := c.Params("code")
	_, err = h.invites.Use(c, code)
	if err != nil {
		return h.mapInviteError(c, err)
	}

	// Check minimum account age requirement.
	cfg, err := h.onboarding.Get(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "invite").Msg("get onboarding config failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if cfg.MinAccountAgeSeconds > 0 {
		u, err := h.users.GetByID(c, userID)
		if err != nil {
			h.log.Error().Err(err).Str("handler", "invite").Msg("get user failed")
			return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
		}
		accountAge := time.Since(u.CreatedAt)
		if accountAge < time.Duration(cfg.MinAccountAgeSeconds)*time.Second {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError,
				"Your account is too new to join this server")
		}
	}

	m, err := h.members.CreatePending(c, userID)
	if err != nil {
		return h.mapInviteError(c, err)
	}

	return httputil.Success(c, m.ToModel())
}

// toInviteModel converts the internal invite type to the protocol response type.
func toInviteModel(inv *invite.Invite) models.Invite {
	result := models.Invite{
		ID:            inv.ID.String(),
		Code:          inv.Code,
		ChannelID:     inv.ChannelID.String(),
		CreatorID:     inv.CreatorID.String(),
		MaxUses:       inv.MaxUses,
		UseCount:      inv.UseCount,
		MaxAgeSeconds: inv.MaxAgeSeconds,
		CreatedAt:     inv.CreatedAt.Format(time.RFC3339),
	}
	if inv.ExpiresAt != nil {
		s := inv.ExpiresAt.Format(time.RFC3339)
		result.ExpiresAt = &s
	}
	return result
}

// mapInviteError converts invite and member layer errors to appropriate HTTP responses.
func (h *InviteHandler) mapInviteError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, invite.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownInvite, "Invite not found")
	case errors.Is(err, invite.ErrExpired):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invite has expired")
	case errors.Is(err, invite.ErrMaxUsesReached):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invite has reached its maximum number of uses")
	case errors.Is(err, invite.ErrChannelNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownChannel, "Channel not found")
	case errors.Is(err, invite.ErrInvalidMaxUses):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, invite.ErrInvalidMaxAge):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, member.ErrAlreadyMember):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.AlreadyMember, "You are already a member of this server")
	default:
		h.log.Error().Err(err).Str("handler", "invite").Msg("unhandled invite service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
