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
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/channel"
	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/onboarding"
	"github.com/uncord-chat/uncord-server/internal/permission"
)

// ChannelHandler serves channel endpoints.
type ChannelHandler struct {
	channels    channel.Repository
	members     member.Repository
	onboarding  onboarding.Repository
	resolver    *permission.Resolver
	gateway     *gateway.Publisher
	maxChannels int
	log         zerolog.Logger
}

// NewChannelHandler creates a new channel handler.
func NewChannelHandler(
	channels channel.Repository,
	members member.Repository,
	onboardingRepo onboarding.Repository,
	resolver *permission.Resolver,
	gw *gateway.Publisher,
	maxChannels int,
	logger zerolog.Logger,
) *ChannelHandler {
	return &ChannelHandler{
		channels:    channels,
		members:     members,
		onboarding:  onboardingRepo,
		resolver:    resolver,
		gateway:     gw,
		maxChannels: maxChannels,
		log:         logger,
	}
}

// ListChannels handles GET /api/v1/server/channels. Active and timed-out members see all channels they have permission
// to view. Pending members see only the welcome channel (if configured). Non-members see an empty list.
func (h *ChannelHandler) ListChannels(c fiber.Ctx) error {
	userID, ok := c.Locals("userID").(uuid.UUID)
	if !ok {
		return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
	}

	status, err := h.members.GetStatus(c, userID)
	if err != nil {
		if errors.Is(err, member.ErrNotFound) {
			return httputil.Success(c, []models.Channel{})
		}
		h.log.Error().Err(err).Str("handler", "channel").Msg("get member status failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	if status == models.MemberStatusPending {
		return h.listWelcomeChannel(c)
	}

	return h.listPermittedChannels(c, userID)
}

// listPermittedChannels returns all channels the user has ViewChannels permission for.
func (h *ChannelHandler) listPermittedChannels(c fiber.Ctx, userID uuid.UUID) error {
	all, err := h.channels.List(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "channel").Msg("list channels failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	channelIDs := make([]uuid.UUID, len(all))
	for i := range all {
		channelIDs[i] = all[i].ID
	}

	permitted, err := h.resolver.FilterPermitted(c, userID, channelIDs, permissions.ViewChannels)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "channel").Msg("permission check failed during channel list")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Channel, 0, len(all))
	for i := range all {
		if permitted[i] {
			result = append(result, all[i].ToModel())
		}
	}
	return httputil.Success(c, result)
}

// listWelcomeChannel returns only the configured welcome channel for pending members.
func (h *ChannelHandler) listWelcomeChannel(c fiber.Ctx) error {
	cfg, err := h.onboarding.Get(c)
	if err != nil {
		h.log.Error().Err(err).Str("handler", "channel").Msg("get onboarding config failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if cfg.WelcomeChannelID == nil {
		return httputil.Success(c, []models.Channel{})
	}

	ch, err := h.channels.GetByID(c, *cfg.WelcomeChannelID)
	if err != nil {
		if errors.Is(err, channel.ErrNotFound) {
			return httputil.Success(c, []models.Channel{})
		}
		h.log.Error().Err(err).Str("handler", "channel").Msg("get welcome channel failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	return httputil.Success(c, []models.Channel{ch.ToModel()})
}

// CreateChannel handles POST /api/v1/server/channels.
func (h *ChannelHandler) CreateChannel(c fiber.Ctx) error {
	var body models.CreateChannelRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	name, err := channel.ValidateNameRequired(body.Name)
	if err != nil {
		return h.mapChannelError(c, err)
	}

	chType := models.ChannelTypeText
	if body.Type != nil {
		chType = *body.Type
	}
	if err := channel.ValidateType(chType); err != nil {
		return h.mapChannelError(c, err)
	}

	if err := channel.ValidateTopic(body.Topic); err != nil {
		return h.mapChannelError(c, err)
	}
	if err := channel.ValidateSlowmode(body.SlowmodeSeconds); err != nil {
		return h.mapChannelError(c, err)
	}

	var categoryID *uuid.UUID
	if body.CategoryID != nil {
		parsed, err := uuid.Parse(*body.CategoryID)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid category ID format")
		}
		categoryID = &parsed
	}

	var topic string
	if body.Topic != nil {
		topic = *body.Topic
	}
	var slowmode int
	if body.SlowmodeSeconds != nil {
		slowmode = *body.SlowmodeSeconds
	}
	var nsfw bool
	if body.NSFW != nil {
		nsfw = *body.NSFW
	}

	ch, err := h.channels.Create(c, channel.CreateParams{
		Name:            name,
		Type:            chType,
		CategoryID:      categoryID,
		Topic:           topic,
		SlowmodeSeconds: slowmode,
		NSFW:            nsfw,
	}, h.maxChannels)
	if err != nil {
		return h.mapChannelError(c, err)
	}

	result := ch.ToModel()
	if h.gateway != nil {
		go func() {
			if err := h.gateway.Publish(context.Background(), events.ChannelCreate, result); err != nil {
				h.log.Warn().Err(err).Str("channel_id", ch.ID.String()).Msg("Gateway publish failed")
			}
		}()
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, result)
}

// GetChannel handles GET /api/v1/channels/:channelID.
func (h *ChannelHandler) GetChannel(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	ch, err := h.channels.GetByID(c, id)
	if err != nil {
		return h.mapChannelError(c, err)
	}

	return httputil.Success(c, ch.ToModel())
}

// UpdateChannel handles PATCH /api/v1/channels/:channelID.
func (h *ChannelHandler) UpdateChannel(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	var body models.UpdateChannelRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	if err := channel.ValidateName(body.Name); err != nil {
		return h.mapChannelError(c, err)
	}
	if err := channel.ValidateTopic(body.Topic); err != nil {
		return h.mapChannelError(c, err)
	}
	if err := channel.ValidatePosition(body.Position); err != nil {
		return h.mapChannelError(c, err)
	}
	if err := channel.ValidateSlowmode(body.SlowmodeSeconds); err != nil {
		return h.mapChannelError(c, err)
	}

	params := channel.UpdateParams{
		Name:            body.Name,
		Topic:           body.Topic,
		Position:        body.Position,
		SlowmodeSeconds: body.SlowmodeSeconds,
		NSFW:            body.NSFW,
	}

	// Interpret CategoryID: nil = no change, "" = remove from category, valid UUID = move to category.
	if body.CategoryID != nil {
		if *body.CategoryID == "" {
			params.SetCategoryNull = true
		} else {
			parsed, err := uuid.Parse(*body.CategoryID)
			if err != nil {
				return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid category ID format")
			}
			params.CategoryID = &parsed
		}
	}

	ch, err := h.channels.Update(c, id, params)
	if err != nil {
		return h.mapChannelError(c, err)
	}

	result := ch.ToModel()
	if h.gateway != nil {
		go func() {
			if err := h.gateway.Publish(context.Background(), events.ChannelUpdate, result); err != nil {
				h.log.Warn().Err(err).Str("channel_id", id.String()).Msg("Gateway publish failed")
			}
		}()
	}

	return httputil.Success(c, result)
}

// DeleteChannel handles DELETE /api/v1/channels/:channelID.
func (h *ChannelHandler) DeleteChannel(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("channelID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidChannelID, "Invalid channel ID format")
	}

	if err := h.channels.Delete(c, id); err != nil {
		return h.mapChannelError(c, err)
	}

	if h.gateway != nil {
		go func() {
			if err := h.gateway.Publish(context.Background(), events.ChannelDelete, models.ChannelDeleteData{ID: id.String()}); err != nil {
				h.log.Warn().Err(err).Str("channel_id", id.String()).Msg("Gateway publish failed")
			}
		}()
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// mapChannelError converts channel-layer errors to appropriate HTTP responses.
func (h *ChannelHandler) mapChannelError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, channel.ErrNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownChannel, "Channel not found")
	case errors.Is(err, channel.ErrNameLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, channel.ErrInvalidType):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, channel.ErrTopicLength):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, channel.ErrInvalidSlowmode):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, channel.ErrInvalidPosition):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, err.Error())
	case errors.Is(err, channel.ErrCategoryNotFound):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.UnknownCategory, err.Error())
	case errors.Is(err, channel.ErrMaxChannelsReached):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.MaxChannelsReached, err.Error())
	default:
		h.log.Error().Err(err).Str("handler", "channel").Msg("unhandled channel service error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}
