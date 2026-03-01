package e2ee

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for the e2ee package.
var (
	ErrDeviceNotFound       = errors.New("device not found")
	ErrSignedPreKeyNotFound = errors.New("signed pre-key not found")
	ErrNoOneTimePreKeys     = errors.New("no one-time pre-keys available")
	ErrKeyBundleIncomplete  = errors.New("device has not uploaded a complete key bundle")
	ErrNoDevices            = errors.New("target user has no registered devices")
	ErrInvalidKeyLength     = errors.New("public key must be exactly 32 bytes")
	ErrInvalidSignature     = errors.New("signature must be exactly 64 bytes")
	ErrDuplicateKeyID       = errors.New("key ID already exists for this device")
	ErrBatchTooLarge        = errors.New("one-time pre-key batch exceeds the maximum size")
	ErrDuplicateDevice      = errors.New("device already registered for this user")
	ErrMaxDevicesReached    = errors.New("user has reached the maximum number of devices")
)

// Key material size constants.
const (
	KeyLength         = 32  // X25519 public key length in bytes.
	SignatureLength   = 64  // Ed25519 signature length in bytes.
	MaxOPKBatch       = 100 // Maximum one-time pre-keys per upload batch.
	LowOPKThreshold   = 10  // Default threshold for KEY_BUNDLE_LOW notifications.
	MaxDevicesPerUser = 5   // Default maximum devices per user.
)

// Device represents a registered device with its long-term X25519 identity key.
type Device struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	DeviceID    uuid.UUID
	Label       *string
	IdentityKey []byte
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SignedPreKey holds a medium-term X25519 public key signed by the device's identity key.
type SignedPreKey struct {
	ID        uuid.UUID
	DeviceID  uuid.UUID
	KeyID     int
	PublicKey []byte
	Signature []byte
	Active    bool
	CreatedAt time.Time
}

// OneTimePreKey holds an ephemeral X25519 public key consumed once during X3DH session initiation.
type OneTimePreKey struct {
	ID        uuid.UUID
	DeviceID  uuid.UUID
	KeyID     int
	PublicKey []byte
	CreatedAt time.Time
}

// DeviceKeyBundle holds the key material for a single device, consumed during X3DH session establishment.
type DeviceKeyBundle struct {
	Device        Device
	SignedPreKey  SignedPreKey
	OneTimePreKey *OneTimePreKey
}

// UserKeyBundle holds key bundles for all ready devices of a user.
type UserKeyBundle struct {
	UserID  uuid.UUID
	Devices []DeviceKeyBundle
}

// RegisterDeviceParams groups the inputs for registering a new device.
type RegisterDeviceParams struct {
	UserID      uuid.UUID
	DeviceID    uuid.UUID
	Label       *string
	IdentityKey []byte
}

// UploadSignedPreKeyParams groups the inputs for uploading a signed pre-key.
type UploadSignedPreKeyParams struct {
	DeviceRowID uuid.UUID
	KeyID       int
	PublicKey   []byte
	Signature   []byte
}

// UploadOPKParams holds a single one-time pre-key for batch upload.
type UploadOPKParams struct {
	KeyID     int
	PublicKey []byte
}

// ValidatePublicKey checks that a public key is exactly KeyLength bytes.
func ValidatePublicKey(key []byte) error {
	if len(key) != KeyLength {
		return fmt.Errorf("%w: got %d bytes", ErrInvalidKeyLength, len(key))
	}
	return nil
}

// ValidateSignature checks that a signature is exactly SignatureLength bytes.
func ValidateSignature(sig []byte) error {
	if len(sig) != SignatureLength {
		return fmt.Errorf("%w: got %d bytes", ErrInvalidSignature, len(sig))
	}
	return nil
}

// ValidateOPKBatch checks that the batch is non-empty and does not exceed MaxOPKBatch, and that each key is valid.
func ValidateOPKBatch(keys []UploadOPKParams, maxBatch int) error {
	if len(keys) == 0 {
		return fmt.Errorf("%w: batch must not be empty", ErrBatchTooLarge)
	}
	if len(keys) > maxBatch {
		return fmt.Errorf("%w: %d keys exceeds limit of %d", ErrBatchTooLarge, len(keys), maxBatch)
	}
	for i, k := range keys {
		if err := ValidatePublicKey(k.PublicKey); err != nil {
			return fmt.Errorf("key at index %d: %w", i, err)
		}
	}
	return nil
}

// Repository defines the data-access contract for E2EE key management.
type Repository interface {
	// RegisterDevice creates a new device registration for a user.
	RegisterDevice(ctx context.Context, params RegisterDeviceParams) (*Device, error)
	// ListDevices returns all registered devices for a user.
	ListDevices(ctx context.Context, userID uuid.UUID) ([]Device, error)
	// GetDeviceByDeviceID looks up a device by the user-assigned device ID.
	GetDeviceByDeviceID(ctx context.Context, userID, deviceID uuid.UUID) (*Device, error)
	// RemoveDevice deletes a device and cascades to all associated keys.
	RemoveDevice(ctx context.Context, deviceRowID uuid.UUID) error
	// UpdateIdentityKey replaces a device's identity key and returns the updated device.
	UpdateIdentityKey(ctx context.Context, deviceRowID uuid.UUID, identityKey []byte) (*Device, error)

	// UploadSignedPreKey deactivates the current active signed pre-key and stores a new one.
	UploadSignedPreKey(ctx context.Context, params UploadSignedPreKeyParams) error
	// UploadOneTimePreKeys stores a batch of one-time pre-keys for a device.
	UploadOneTimePreKeys(ctx context.Context, deviceRowID uuid.UUID, keys []UploadOPKParams) error
	// CountOneTimePreKeys returns the number of unused one-time pre-keys for a device.
	CountOneTimePreKeys(ctx context.Context, deviceRowID uuid.UUID) (int, error)

	// FetchUserKeyBundle fetches key bundles for all ready devices of a user, atomically consuming one OPK per device.
	FetchUserKeyBundle(ctx context.Context, targetUserID uuid.UUID) (*UserKeyBundle, error)

	// StoreMessageKeys stores per-device encrypted message keys for a DM message.
	StoreMessageKeys(ctx context.Context, messageID uuid.UUID, keys map[uuid.UUID][]byte) error
	// GetMessageKeyForDevice returns the encrypted key for a single message and device.
	GetMessageKeyForDevice(ctx context.Context, messageID, deviceRowID uuid.UUID) ([]byte, error)
	// GetMessageKeysBatch returns encrypted keys for multiple messages for a single device.
	GetMessageKeysBatch(ctx context.Context, messageIDs []uuid.UUID, deviceRowID uuid.UUID) (map[uuid.UUID][]byte, error)
}
