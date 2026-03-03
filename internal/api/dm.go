package api

import (
	"encoding/base64"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/dm"
	"github.com/uncord-chat/uncord-server/internal/e2ee"
	"github.com/uncord-chat/uncord-server/internal/gateway"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/message"
)

// DMHandler serves DM channel endpoints.
type DMHandler struct {
	dms        dm.Repository
	messages   message.Repository
	e2eeKeys   e2ee.Repository
	gateway    *gateway.Publisher
	maxContent int
	log        zerolog.Logger
}

// NewDMHandler creates a new DM handler.
func NewDMHandler(
	dms dm.Repository,
	messages message.Repository,
	e2eeKeys e2ee.Repository,
	gw *gateway.Publisher,
	maxContent int,
	logger zerolog.Logger,
) *DMHandler {
	return &DMHandler{
		dms:        dms,
		messages:   messages,
		e2eeKeys:   e2eeKeys,
		gateway:    gw,
		maxContent: maxContent,
		log:        logger,
	}
}

// RequireParticipant is a middleware that verifies the requesting user is a participant of the DM channel identified by
// the :channelID route parameter.
func (h *DMHandler) RequireParticipant(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	channelID, ok := httputil.ParseUUIDParam(c, "channelID", apierrors.InvalidChannelID)
	if !ok {
		return nil
	}

	ok, err = h.dms.IsParticipant(c, channelID, userID)
	if err != nil {
		h.log.Error().Err(err).Msg("check dm participant failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
	if !ok {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.NotParticipant, "You are not a participant of this DM channel")
	}

	return c.Next()
}

// CreateDMChannel handles POST /api/v1/users/@me/channels.
func (h *DMHandler) CreateDMChannel(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	var body struct {
		RecipientID string   `json:"recipient_id"`
		Name        string   `json:"name"`
		Recipients  []string `json:"recipients"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	// Group DM: name + recipients array.
	if body.Name != "" || len(body.Recipients) > 0 {
		if body.Name == "" {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Group DM requires a name")
		}
		participantIDs := make([]uuid.UUID, len(body.Recipients))
		for i, r := range body.Recipients {
			id, err := uuid.Parse(r)
			if err != nil {
				return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid recipient ID format")
			}
			participantIDs[i] = id
		}

		ch, err := h.dms.CreateGroupDM(c, dm.CreateGroupDMParams{
			OwnerID:        userID,
			Name:           body.Name,
			ParticipantIDs: participantIDs,
		})
		if err != nil {
			return h.mapDMError(c, err)
		}

		participants, pErr := h.dms.ListParticipants(c, ch.ID)
		if pErr != nil {
			h.log.Error().Err(pErr).Msg("list participants for group dm create event failed")
		}
		participantUserIDs := participantUserIDs(participants)
		if h.gateway != nil {
			h.gateway.EnqueueTargeted(events.DMChannelCreate, dmChannelToModel(ch), participantUserIDs)
		}

		return httputil.SuccessStatus(c, fiber.StatusCreated, dmChannelToModel(ch))
	}

	// 1:1 DM: recipient_id.
	recipientID, err := uuid.Parse(body.RecipientID)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid recipient_id format")
	}

	ch, err := h.dms.CreateDM(c, dm.CreateDMParams{
		CreatorID:   userID,
		RecipientID: recipientID,
	})
	if err != nil {
		return h.mapDMError(c, err)
	}

	if h.gateway != nil {
		h.gateway.EnqueueTargeted(events.DMChannelCreate, dmChannelToModel(ch), []uuid.UUID{userID, recipientID})
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, dmChannelToModel(ch))
}

// ListDMChannels handles GET /api/v1/users/@me/channels.
func (h *DMHandler) ListDMChannels(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	channels, err := h.dms.ListForUser(c, userID)
	if err != nil {
		h.log.Error().Err(err).Msg("list dm channels failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]dmChannelModel, len(channels))
	for i := range channels {
		result[i] = dmChannelToModel(&channels[i])
	}
	return httputil.Success(c, result)
}

// GetDMChannel handles GET /api/v1/dm/:channelID.
func (h *DMHandler) GetDMChannel(c fiber.Ctx) error {
	channelID, ok := httputil.ParseUUIDParam(c, "channelID", apierrors.InvalidChannelID)
	if !ok {
		return nil
	}

	ch, err := h.dms.GetByID(c, channelID)
	if err != nil {
		return h.mapDMError(c, err)
	}

	return httputil.Success(c, dmChannelToModel(ch))
}

// AddParticipant handles POST /api/v1/dm/:channelID/participants.
func (h *DMHandler) AddParticipant(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}
	channelID, ok := httputil.ParseUUIDParam(c, "channelID", apierrors.InvalidChannelID)
	if !ok {
		return nil
	}

	ch, err := h.dms.GetByID(c, channelID)
	if err != nil {
		return h.mapDMError(c, err)
	}
	if ch.Type != dm.TypeGroupDM {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Cannot add participants to a 1:1 DM")
	}
	if ch.OwnerID == nil || *ch.OwnerID != userID {
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.NotDMOwner, "Only the group DM owner can add participants")
	}

	var body struct {
		UserID string `json:"user_id"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}
	targetID, err := uuid.Parse(body.UserID)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid user_id format")
	}

	if err := h.dms.AddParticipant(c, channelID, targetID); err != nil {
		return h.mapDMError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// RemoveParticipant handles DELETE /api/v1/dm/:channelID/participants/:userID.
func (h *DMHandler) RemoveParticipant(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}
	channelID, ok := httputil.ParseUUIDParam(c, "channelID", apierrors.InvalidChannelID)
	if !ok {
		return nil
	}

	ch, err := h.dms.GetByID(c, channelID)
	if err != nil {
		return h.mapDMError(c, err)
	}
	if ch.Type != dm.TypeGroupDM {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Cannot remove participants from a 1:1 DM")
	}

	targetID, ok := httputil.ParseUUIDParam(c, "userID", apierrors.ValidationError)
	if !ok {
		return nil
	}

	// Only the owner can remove others. A participant can remove themselves (leave).
	if targetID != userID {
		if ch.OwnerID == nil || *ch.OwnerID != userID {
			return httputil.Fail(c, fiber.StatusForbidden, apierrors.NotDMOwner, "Only the group DM owner can remove participants")
		}
	}

	if err := h.dms.RemoveParticipant(c, channelID, targetID); err != nil {
		return h.mapDMError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ListMessages handles GET /api/v1/dm/:channelID/messages.
func (h *DMHandler) ListMessages(c fiber.Ctx) error {
	channelID, ok := httputil.ParseUUIDParam(c, "channelID", apierrors.InvalidChannelID)
	if !ok {
		return nil
	}

	var before *uuid.UUID
	if raw := c.Query("before"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid before parameter")
		}
		before = &id
	}

	rawLimit, _ := strconv.Atoi(c.Query("limit"))
	limit := message.ClampLimit(rawLimit)

	messages, err := h.messages.List(c, channelID, before, limit)
	if err != nil {
		h.log.Error().Err(err).Msg("list dm messages failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	// Resolve the requesting device for per-device message key delivery.
	deviceRowID := h.resolveDeviceRowID(c)

	result := make([]models.Message, len(messages))
	msgIDs := make([]uuid.UUID, len(messages))
	for i := range messages {
		msgIDs[i] = messages[i].ID
	}

	var keyMap map[uuid.UUID][]byte
	if deviceRowID != nil && h.e2eeKeys != nil {
		var keyErr error
		keyMap, keyErr = h.e2eeKeys.GetMessageKeysBatch(c, msgIDs, *deviceRowID)
		if keyErr != nil {
			h.log.Error().Err(keyErr).Msg("fetch dm message keys batch failed")
		}
	}

	for i := range messages {
		result[i] = dmMessageToModel(&messages[i], keyMap)
	}
	return httputil.Success(c, result)
}

// SendMessage handles POST /api/v1/dm/:channelID/messages.
func (h *DMHandler) SendMessage(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}
	channelID, ok := httputil.ParseUUIDParam(c, "channelID", apierrors.InvalidChannelID)
	if !ok {
		return nil
	}

	var body models.CreateDMMessageRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	content, err := message.ValidateContent(body.Content, h.maxContent)
	if err != nil {
		return mapMessageError(c, err, h.log)
	}

	var replyToID *uuid.UUID
	if body.ReplyToID != nil {
		id, err := uuid.Parse(*body.ReplyToID)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid reply_to_id format")
		}
		replyToID = &id
	}

	msg, err := h.messages.Create(c, message.CreateParams{
		ChannelID: channelID,
		AuthorID:  userID,
		Content:   content,
		ReplyToID: replyToID,
		Encrypted: true,
	})
	if err != nil {
		return mapMessageError(c, err, h.log)
	}

	// Store per-device encrypted message keys if provided.
	if len(body.MessageKeys) > 0 && h.e2eeKeys != nil {
		keys := make(map[uuid.UUID][]byte, len(body.MessageKeys))
		for _, mk := range body.MessageKeys {
			devID, pErr := uuid.Parse(mk.DeviceID)
			if pErr != nil {
				continue
			}
			encKey, dErr := base64.RawStdEncoding.DecodeString(mk.EncryptedKey)
			if dErr != nil {
				continue
			}
			keys[devID] = encKey
		}
		if len(keys) > 0 {
			if err := h.e2eeKeys.StoreMessageKeys(c, msg.ID, keys); err != nil {
				h.log.Warn().Err(err).Msg("store dm message keys failed")
			}
		}
	}

	// Dispatch to participants.
	if h.gateway != nil {
		participants, pErr := h.dms.ListParticipants(c, channelID)
		if pErr != nil {
			h.log.Error().Err(pErr).Msg("list participants for dm message create event failed")
		}
		targets := participantUserIDs(participants)
		msgModel := dmMessageToModel(msg, nil)
		h.gateway.EnqueueTargeted(events.DMMessageCreate, msgModel, targets)
	}

	return httputil.SuccessStatus(c, fiber.StatusCreated, dmMessageToModel(msg, nil))
}

// resolveDeviceRowID extracts the server-assigned device row ID from the X-Device-ID header. The header carries the
// client-generated device UUID, which is resolved to the server-assigned row PK via the user_devices table.
func (h *DMHandler) resolveDeviceRowID(c fiber.Ctx) *uuid.UUID {
	raw := c.Get("X-Device-ID")
	if raw == "" {
		return nil
	}
	deviceID, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	userID, err := httputil.UserID(c)
	if err != nil {
		return nil
	}
	dev, err := h.e2eeKeys.GetDeviceByDeviceID(c, userID, deviceID)
	if err != nil {
		return nil
	}
	return &dev.ID
}

// mapDMError converts dm-layer errors to appropriate HTTP responses.
func (h *DMHandler) mapDMError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, dm.ErrDMNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownDMChannel, "DM channel not found")
	case errors.Is(err, dm.ErrAlreadyParticipant):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.AlreadyParticipant, "User is already a participant")
	case errors.Is(err, dm.ErrNotParticipant):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.NotParticipant, "User is not a participant")
	case errors.Is(err, dm.ErrGroupDMFull):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.GroupDMFull, "Group DM is full")
	case errors.Is(err, dm.ErrNotOwner):
		return httputil.Fail(c, fiber.StatusForbidden, apierrors.NotDMOwner, "Only the group DM owner can perform this action")
	default:
		h.log.Error().Err(err).Msg("unhandled dm error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}

// dmChannelModel is the API response model for a DM channel. It is defined locally rather than in the protocol module
// because DM channels have a different field set from server channels (no category, topic, position, or permission
// overwrites). The protocol module's Channel type models server channels only; a shared type would either carry unused
// fields or require a discriminated union that adds complexity without benefit.
type dmChannelModel struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Name      *string `json:"name,omitempty"`
	OwnerID   *string `json:"owner_id,omitempty"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func dmChannelToModel(ch *dm.Channel) dmChannelModel {
	m := dmChannelModel{
		ID:        ch.ID.String(),
		Type:      ch.Type,
		Name:      ch.Name,
		CreatedAt: ch.CreatedAt.Format(time.RFC3339),
		UpdatedAt: ch.UpdatedAt.Format(time.RFC3339),
	}
	if ch.OwnerID != nil {
		s := ch.OwnerID.String()
		m.OwnerID = &s
	}
	return m
}

// dmMessageToModel converts a message to a protocol model, attaching the requesting device's encrypted key if
// available. The keyMap is keyed by message ID.
func dmMessageToModel(m *message.Message, keyMap map[uuid.UUID][]byte) models.Message {
	var replyToID *string
	if m.ReplyToID != nil {
		s := m.ReplyToID.String()
		replyToID = &s
	}
	var editedAt *string
	if m.EditedAt != nil {
		s := m.EditedAt.Format(time.RFC3339)
		editedAt = &s
	}

	msg := models.Message{
		ID:        m.ID.String(),
		ChannelID: m.ChannelID.String(),
		Author: models.MemberUser{
			ID:          m.AuthorID.String(),
			Username:    m.AuthorUsername,
			DisplayName: m.AuthorDisplayName,
			AvatarKey:   m.AuthorAvatarKey,
		},
		Content:     m.Content,
		Attachments: []models.Attachment{},
		Reactions:   []models.ReactionSummary{},
		ReplyToID:   replyToID,
		Pinned:      m.Pinned,
		Encrypted:   m.Encrypted,
		EditedAt:    editedAt,
		CreatedAt:   m.CreatedAt.Format(time.RFC3339),
	}

	if key, ok := keyMap[m.ID]; ok {
		msg.MessageKeys = []models.EncryptedMessageKey{{
			DeviceID:     "", // The client already knows its own device ID.
			EncryptedKey: base64.RawStdEncoding.EncodeToString(key),
		}}
	}

	return msg
}

// participantUserIDs extracts user IDs from a slice of participants.
func participantUserIDs(participants []dm.Participant) []uuid.UUID {
	ids := make([]uuid.UUID, len(participants))
	for i := range participants {
		ids[i] = participants[i].UserID
	}
	return ids
}
