package api

import (
	"encoding/base64"
	"errors"
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
)

// E2EEHandler serves device and key management endpoints for end-to-end encryption.
type E2EEHandler struct {
	keys         e2ee.Repository
	dms          dm.Repository
	gateway      *gateway.Publisher
	opkThreshold int
	log          zerolog.Logger
}

// NewE2EEHandler creates a new E2EE handler.
func NewE2EEHandler(keys e2ee.Repository, dms dm.Repository, gw *gateway.Publisher, opkThreshold int, logger zerolog.Logger) *E2EEHandler {
	return &E2EEHandler{
		keys:         keys,
		dms:          dms,
		gateway:      gw,
		opkThreshold: opkThreshold,
		log:          logger,
	}
}

// RegisterDevice handles POST /api/v1/users/@me/devices.
func (h *E2EEHandler) RegisterDevice(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	var body models.RegisterDeviceRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	deviceID, err := uuid.Parse(body.DeviceID)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid device_id format")
	}

	identityKey, err := base64.RawStdEncoding.DecodeString(body.IdentityKey)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidKeyMaterial, "Invalid base64 encoding for identity_key")
	}
	if err := e2ee.ValidatePublicKey(identityKey); err != nil {
		return h.mapE2EEError(c, err)
	}

	var label *string
	if body.Label != "" {
		label = &body.Label
	}

	dev, err := h.keys.RegisterDevice(c, e2ee.RegisterDeviceParams{
		UserID:      userID,
		DeviceID:    deviceID,
		Label:       label,
		IdentityKey: identityKey,
	})
	if err != nil {
		return h.mapE2EEError(c, err)
	}

	h.notifyIdentityKeyChanged(c, userID, deviceID, identityKey)

	return httputil.SuccessStatus(c, fiber.StatusCreated, deviceToModel(dev))
}

// ListDevices handles GET /api/v1/users/@me/devices.
func (h *E2EEHandler) ListDevices(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	devices, err := h.keys.ListDevices(c, userID)
	if err != nil {
		h.log.Error().Err(err).Msg("list devices failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	result := make([]models.Device, len(devices))
	for i := range devices {
		result[i] = deviceToModel(&devices[i])
	}
	return httputil.Success(c, result)
}

// RemoveDevice handles DELETE /api/v1/users/@me/devices/:deviceID.
func (h *E2EEHandler) RemoveDevice(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	deviceID, err := uuid.Parse(c.Params("deviceID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid device ID format")
	}

	dev, err := h.keys.GetDeviceByDeviceID(c, userID, deviceID)
	if err != nil {
		return h.mapE2EEError(c, err)
	}

	if err := h.keys.RemoveDevice(c, dev.ID); err != nil {
		return h.mapE2EEError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// UpdateIdentityKey handles PUT /api/v1/users/@me/devices/:deviceID/identity-key.
func (h *E2EEHandler) UpdateIdentityKey(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	deviceID, err := uuid.Parse(c.Params("deviceID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid device ID format")
	}

	var body models.UpdateIdentityKeyRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	identityKey, err := base64.RawStdEncoding.DecodeString(body.IdentityKey)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidKeyMaterial, "Invalid base64 encoding for identity_key")
	}
	if err := e2ee.ValidatePublicKey(identityKey); err != nil {
		return h.mapE2EEError(c, err)
	}

	dev, err := h.keys.GetDeviceByDeviceID(c, userID, deviceID)
	if err != nil {
		return h.mapE2EEError(c, err)
	}

	if _, err := h.keys.UpdateIdentityKey(c, dev.ID, identityKey); err != nil {
		return h.mapE2EEError(c, err)
	}

	h.notifyIdentityKeyChanged(c, userID, deviceID, identityKey)

	return c.SendStatus(fiber.StatusNoContent)
}

// UploadSignedPreKey handles PUT /api/v1/users/@me/devices/:deviceID/signed-pre-key.
func (h *E2EEHandler) UploadSignedPreKey(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	deviceID, err := uuid.Parse(c.Params("deviceID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid device ID format")
	}

	var body models.UploadSignedPreKeyRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	publicKey, err := base64.RawStdEncoding.DecodeString(body.PublicKey)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidKeyMaterial, "Invalid base64 encoding for public_key")
	}
	if err := e2ee.ValidatePublicKey(publicKey); err != nil {
		return h.mapE2EEError(c, err)
	}

	signature, err := base64.RawStdEncoding.DecodeString(body.Signature)
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidKeyMaterial, "Invalid base64 encoding for signature")
	}
	if err := e2ee.ValidateSignature(signature); err != nil {
		return h.mapE2EEError(c, err)
	}

	dev, err := h.keys.GetDeviceByDeviceID(c, userID, deviceID)
	if err != nil {
		return h.mapE2EEError(c, err)
	}

	if err := h.keys.UploadSignedPreKey(c, e2ee.UploadSignedPreKeyParams{
		DeviceRowID: dev.ID,
		KeyID:       body.KeyID,
		PublicKey:   publicKey,
		Signature:   signature,
	}); err != nil {
		return h.mapE2EEError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// UploadOneTimePreKeys handles POST /api/v1/users/@me/devices/:deviceID/one-time-pre-keys.
func (h *E2EEHandler) UploadOneTimePreKeys(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	deviceID, err := uuid.Parse(c.Params("deviceID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid device ID format")
	}

	var body models.UploadOneTimePreKeysRequest
	if err := c.Bind().Body(&body); err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidBody, "Invalid request body")
	}

	keys := make([]e2ee.UploadOPKParams, len(body.PreKeys))
	for i, pk := range body.PreKeys {
		decoded, err := base64.RawStdEncoding.DecodeString(pk.PublicKey)
		if err != nil {
			return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidKeyMaterial, "Invalid base64 encoding for pre_key public_key")
		}
		keys[i] = e2ee.UploadOPKParams{KeyID: pk.KeyID, PublicKey: decoded}
	}

	if err := e2ee.ValidateOPKBatch(keys, e2ee.MaxOPKBatch); err != nil {
		return h.mapE2EEError(c, err)
	}

	dev, err := h.keys.GetDeviceByDeviceID(c, userID, deviceID)
	if err != nil {
		return h.mapE2EEError(c, err)
	}

	if err := h.keys.UploadOneTimePreKeys(c, dev.ID, keys); err != nil {
		return h.mapE2EEError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GetKeyCount handles GET /api/v1/users/@me/devices/:deviceID/one-time-pre-keys/count.
func (h *E2EEHandler) GetKeyCount(c fiber.Ctx) error {
	userID, err := httputil.UserID(c)
	if err != nil {
		return err
	}

	deviceID, err := uuid.Parse(c.Params("deviceID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid device ID format")
	}

	dev, err := h.keys.GetDeviceByDeviceID(c, userID, deviceID)
	if err != nil {
		return h.mapE2EEError(c, err)
	}

	count, err := h.keys.CountOneTimePreKeys(c, dev.ID)
	if err != nil {
		h.log.Error().Err(err).Msg("count one-time pre-keys failed")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}

	return httputil.Success(c, models.KeyCountResponse{Count: count})
}

// FetchKeyBundle handles GET /api/v1/users/:userID/keys.
func (h *E2EEHandler) FetchKeyBundle(c fiber.Ctx) error {
	targetUserID, err := uuid.Parse(c.Params("userID"))
	if err != nil {
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.ValidationError, "Invalid user ID format")
	}

	bundle, err := h.keys.FetchUserKeyBundle(c, targetUserID)
	if err != nil {
		return h.mapE2EEError(c, err)
	}

	// Check OPK counts for each device and fire KEY_BUNDLE_LOW if needed.
	if h.gateway != nil {
		for _, db := range bundle.Devices {
			count, cErr := h.keys.CountOneTimePreKeys(c, db.Device.ID)
			if cErr != nil {
				h.log.Warn().Err(cErr).Msg("count opk for low notification failed")
				continue
			}
			if count <= h.opkThreshold {
				h.gateway.EnqueueTargeted(events.KeyBundleLow, models.KeyBundleLowData{
					DeviceID:  db.Device.DeviceID.String(),
					Remaining: count,
				}, []uuid.UUID{targetUserID})
			}
		}
	}

	return httputil.Success(c, bundleToModel(bundle))
}

// notifyIdentityKeyChanged fires an IDENTITY_KEY_CHANGED event to all DM peers of the user.
func (h *E2EEHandler) notifyIdentityKeyChanged(c fiber.Ctx, userID, deviceID uuid.UUID, identityKey []byte) {
	if h.gateway == nil || h.dms == nil {
		return
	}

	peers, err := h.dms.ListDMPeers(c, userID)
	if err != nil {
		h.log.Warn().Err(err).Msg("list dm peers for identity key notification failed")
		return
	}
	if len(peers) == 0 {
		return
	}

	h.gateway.EnqueueTargeted(events.IdentityKeyChanged, models.IdentityKeyChangedData{
		UserID:      userID.String(),
		DeviceID:    deviceID.String(),
		IdentityKey: base64.RawStdEncoding.EncodeToString(identityKey),
	}, peers)
}

// mapE2EEError converts e2ee-layer errors to appropriate HTTP responses.
func (h *E2EEHandler) mapE2EEError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, e2ee.ErrInvalidKeyLength), errors.Is(err, e2ee.ErrInvalidSignature):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.InvalidKeyMaterial, err.Error())
	case errors.Is(err, e2ee.ErrBatchTooLarge):
		return httputil.Fail(c, fiber.StatusBadRequest, apierrors.OPKBatchTooLarge, err.Error())
	case errors.Is(err, e2ee.ErrDuplicateKeyID), errors.Is(err, e2ee.ErrDuplicateDevice):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.DuplicateKeyID, err.Error())
	case errors.Is(err, e2ee.ErrMaxDevicesReached):
		return httputil.Fail(c, fiber.StatusConflict, apierrors.MaxDevicesReached, err.Error())
	case errors.Is(err, e2ee.ErrKeyBundleIncomplete), errors.Is(err, e2ee.ErrNoDevices):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.KeyBundleIncomplete, "No complete key bundles available")
	case errors.Is(err, e2ee.ErrDeviceNotFound):
		return httputil.Fail(c, fiber.StatusNotFound, apierrors.UnknownDevice, "Device not found")
	default:
		h.log.Error().Err(err).Msg("unhandled e2ee error")
		return httputil.Fail(c, fiber.StatusInternalServerError, apierrors.InternalError, "An internal error occurred")
	}
}

// deviceToModel converts an internal device to a protocol response model.
func deviceToModel(d *e2ee.Device) models.Device {
	m := models.Device{
		ID:          d.ID.String(),
		DeviceID:    d.DeviceID.String(),
		IdentityKey: base64.RawStdEncoding.EncodeToString(d.IdentityKey),
		CreatedAt:   d.CreatedAt.Format(time.RFC3339),
	}
	if d.Label != nil {
		m.Label = *d.Label
	}
	return m
}

// bundleToModel converts an internal user key bundle to a protocol response model.
func bundleToModel(b *e2ee.UserKeyBundle) models.UserKeyBundleResponse {
	devices := make([]models.DeviceKeyBundleResponse, len(b.Devices))
	for i, db := range b.Devices {
		resp := models.DeviceKeyBundleResponse{
			DeviceID:       db.Device.DeviceID.String(),
			IdentityKey:    base64.RawStdEncoding.EncodeToString(db.Device.IdentityKey),
			SignedPreKeyID: db.SignedPreKey.KeyID,
			SignedPreKey:   base64.RawStdEncoding.EncodeToString(db.SignedPreKey.PublicKey),
			Signature:      base64.RawStdEncoding.EncodeToString(db.SignedPreKey.Signature),
		}
		if db.OneTimePreKey != nil {
			resp.OneTimeKeyID = &db.OneTimePreKey.KeyID
			opk := base64.RawStdEncoding.EncodeToString(db.OneTimePreKey.PublicKey)
			resp.OneTimeKey = &opk
		}
		devices[i] = resp
	}
	return models.UserKeyBundleResponse{
		UserID:  b.UserID.String(),
		Devices: devices,
	}
}
