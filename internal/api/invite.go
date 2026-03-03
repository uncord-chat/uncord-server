package api

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/audit"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/invite"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/onboarding"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// InviteHandler serves invite endpoints.
type InviteHandler struct {
	invites     invite.Repository
	onboarding  onboarding.Repository
	members     member.Repository
	users       user.Repository
	auditLogger *audit.Logger
	log         zerolog.Logger
}

// NewInviteHandler creates a new invite handler.
func NewInviteHandler(invites invite.Repository, onboardingRepo onboarding.Repository, members member.Repository, users user.Repository, auditLogger *audit.Logger, logger zerolog.Logger) *InviteHandler {
	return &InviteHandler{invites: invites, onboarding: onboardingRepo, members: members, users: users, auditLogger: auditLogger, log: logger}
}

// CreateInvite handles POST /api/v1/server/invites.
func (h *InviteHandler) CreateInvite(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
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

	if h.auditLogger != nil {
		go h.auditLogger.Record(context.Background(), audit.Entry{
			ActorID: audit.UUIDPtr(userID), Action: audit.InviteCreate,
			TargetType: audit.Ptr("invite"), TargetID: audit.UUIDPtr(inv.ID),
		})
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
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	code := c.Params("code")
	if err := h.invites.Delete(c, code); err != nil {
		return h.mapInviteError(c, err)
	}

	if h.auditLogger != nil {
		go h.auditLogger.Record(context.Background(), audit.Entry{
			ActorID: audit.UUIDPtr(userID), Action: audit.InviteDelete,
			TargetType: audit.Ptr("invite"),
			Changes:    audit.MarshalChanges(map[string]string{"code": code}),
		})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// JoinViaInvite handles POST /api/v1/invites/:code/join.
func (h *InviteHandler) JoinViaInvite(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
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

	// Validate the invite before checking email verification so that clients can distinguish "invite not found" from
	// "email not verified". An invalid code returns UNKNOWN_INVITE; only a confirmed-valid invite can produce
	// EMAIL_NOT_VERIFIED.
	code := c.Params("code")
	inv, err := h.invites.GetByCode(c, code)
	if err != nil {
		return h.mapInviteError(c, err)
	}
	if err := inv.Validate(); err != nil {
		return h.mapInviteError(c, err)
	}

	// Fetch the user once for both the email verification and account age checks.
	u, err := h.users.GetByID(c, userID)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "invite").Msg("get user failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if !u.EmailVerified {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.EmailNotVerified, "Email verification is required")
	}

	// Check minimum account age requirement before consuming the invite so that a rejected join does not waste an
	// invite use.
	cfg, err := h.onboarding.Get(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "invite").Msg("get onboarding config failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if cfg.MinAccountAgeSeconds > 0 {
		accountAge := time.Since(u.CreatedAt)
		if accountAge < time.Duration(cfg.MinAccountAgeSeconds)*time.Second {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError,
				"Your account is too new to join this server")
		}
	}

	// Atomically consume the invite. The invite may have become invalid between the validation above and this call
	// (e.g. another user consumed the last use), which is handled correctly by Use.
	if _, err := h.invites.Use(c, code); err != nil {
		return h.mapInviteError(c, err)
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
