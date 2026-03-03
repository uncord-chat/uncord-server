package e2ee

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uncord-chat/uncord-server/internal/postgres"
)

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db         *pgxpool.Pool
	maxDevices int
}

// NewPGRepository returns a new PostgreSQL-backed E2EE repository.
func NewPGRepository(db *pgxpool.Pool, maxDevices int) *PGRepository {
	return &PGRepository{db: db, maxDevices: maxDevices}
}

// RegisterDevice creates a new device registration for a user. The count check and insert run inside a single
// transaction to prevent concurrent requests from exceeding the per-user device limit. Returns ErrDuplicateDevice on a
// (user_id, device_id) conflict.
func (r *PGRepository) RegisterDevice(ctx context.Context, params RegisterDeviceParams) (*Device, error) {
	var d Device
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM user_devices WHERE user_id = $1 FOR UPDATE`,
			params.UserID).Scan(&count); err != nil {
			return fmt.Errorf("count devices: %w", err)
		}
		if count >= r.maxDevices {
			return ErrMaxDevicesReached
		}

		err := tx.QueryRow(ctx,
			`INSERT INTO user_devices (user_id, device_id, label, identity_key)
			 VALUES ($1, $2, $3, $4)
			 RETURNING id, user_id, device_id, label, identity_key, created_at, updated_at`,
			params.UserID, params.DeviceID, params.Label, params.IdentityKey,
		).Scan(&d.ID, &d.UserID, &d.DeviceID, &d.Label, &d.IdentityKey, &d.CreatedAt, &d.UpdatedAt)
		if err != nil {
			if postgres.IsUniqueViolation(err) {
				return ErrDuplicateDevice
			}
			return fmt.Errorf("insert device: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListDevices returns all registered devices for a user, ordered by creation time.
func (r *PGRepository) ListDevices(ctx context.Context, userID uuid.UUID) ([]Device, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, device_id, label, identity_key, created_at, updated_at
		 FROM user_devices WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.UserID, &d.DeviceID, &d.Label, &d.IdentityKey, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		devices = append(devices, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	return devices, nil
}

// GetDeviceByDeviceID looks up a device by (user_id, device_id) pair.
func (r *PGRepository) GetDeviceByDeviceID(ctx context.Context, userID, deviceID uuid.UUID) (*Device, error) {
	var d Device
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, device_id, label, identity_key, created_at, updated_at
		 FROM user_devices WHERE user_id = $1 AND device_id = $2`, userID, deviceID,
	).Scan(&d.ID, &d.UserID, &d.DeviceID, &d.Label, &d.IdentityKey, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDeviceNotFound
		}
		return nil, fmt.Errorf("get device: %w", err)
	}
	return &d, nil
}

// RemoveDevice deletes a device and cascades to all associated SPKs, OPKs, and message keys.
func (r *PGRepository) RemoveDevice(ctx context.Context, deviceRowID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM user_devices WHERE id = $1`, deviceRowID)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

// UpdateIdentityKey replaces a device's identity key and returns the updated device.
func (r *PGRepository) UpdateIdentityKey(ctx context.Context, deviceRowID uuid.UUID, identityKey []byte) (*Device, error) {
	var d Device
	err := r.db.QueryRow(ctx,
		`UPDATE user_devices SET identity_key = $1 WHERE id = $2
		 RETURNING id, user_id, device_id, label, identity_key, created_at, updated_at`,
		identityKey, deviceRowID,
	).Scan(&d.ID, &d.UserID, &d.DeviceID, &d.Label, &d.IdentityKey, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDeviceNotFound
		}
		return nil, fmt.Errorf("update identity key: %w", err)
	}
	return &d, nil
}

// UploadSignedPreKey deactivates the current active signed pre-key for the device and stores a new one as active.
func (r *PGRepository) UploadSignedPreKey(ctx context.Context, params UploadSignedPreKeyParams) error {
	return postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE e2ee_signed_pre_keys SET active = false WHERE device_id = $1 AND active = true`,
			params.DeviceRowID)
		if err != nil {
			return fmt.Errorf("deactivate old spk: %w", err)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO e2ee_signed_pre_keys (device_id, key_id, public_key, signature, active)
			 VALUES ($1, $2, $3, $4, true)`,
			params.DeviceRowID, params.KeyID, params.PublicKey, params.Signature)
		if err != nil {
			if postgres.IsUniqueViolation(err) {
				return ErrDuplicateKeyID
			}
			return fmt.Errorf("insert spk: %w", err)
		}
		return nil
	})
}

// UploadOneTimePreKeys stores a batch of one-time pre-keys for a device using CopyFrom.
func (r *PGRepository) UploadOneTimePreKeys(ctx context.Context, deviceRowID uuid.UUID, keys []UploadOPKParams) error {
	rows := make([][]any, len(keys))
	for i, k := range keys {
		rows[i] = []any{deviceRowID, k.KeyID, k.PublicKey}
	}

	_, err := r.db.CopyFrom(
		ctx,
		pgx.Identifier{"e2ee_one_time_pre_keys"},
		[]string{"device_id", "key_id", "public_key"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return ErrDuplicateKeyID
		}
		return fmt.Errorf("copy one-time pre-keys: %w", err)
	}
	return nil
}

// CountOneTimePreKeys returns the number of unused one-time pre-keys for a device.
func (r *PGRepository) CountOneTimePreKeys(ctx context.Context, deviceRowID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM e2ee_one_time_pre_keys WHERE device_id = $1`, deviceRowID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count one-time pre-keys: %w", err)
	}
	return count, nil
}

// FetchUserKeyBundle fetches key bundles for all ready devices of a user. The entire operation runs inside a single
// transaction to provide a consistent snapshot and serialise OPK consumption. For each device that has an active signed
// pre-key, it atomically consumes one OPK using DELETE ... LIMIT 1 with FOR UPDATE SKIP LOCKED. Devices without an
// active SPK are skipped. Returns ErrNoDevices if the user has no registered devices at all.
func (r *PGRepository) FetchUserKeyBundle(ctx context.Context, targetUserID uuid.UUID) (*UserKeyBundle, error) {
	var bundle *UserKeyBundle
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id, user_id, device_id, label, identity_key, created_at, updated_at
			 FROM user_devices WHERE user_id = $1 ORDER BY created_at`, targetUserID)
		if err != nil {
			return fmt.Errorf("list devices: %w", err)
		}
		defer rows.Close()

		var devices []Device
		for rows.Next() {
			var d Device
			if err := rows.Scan(&d.ID, &d.UserID, &d.DeviceID, &d.Label, &d.IdentityKey, &d.CreatedAt,
				&d.UpdatedAt); err != nil {
				return fmt.Errorf("scan device: %w", err)
			}
			devices = append(devices, d)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate devices: %w", err)
		}
		if len(devices) == 0 {
			return ErrNoDevices
		}

		var bundles []DeviceKeyBundle
		for _, dev := range devices {
			var spk SignedPreKey
			err := tx.QueryRow(ctx,
				`SELECT id, device_id, key_id, public_key, signature, active, created_at
				 FROM e2ee_signed_pre_keys WHERE device_id = $1 AND active = true LIMIT 1`, dev.ID,
			).Scan(&spk.ID, &spk.DeviceID, &spk.KeyID, &spk.PublicKey, &spk.Signature, &spk.Active, &spk.CreatedAt)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				return fmt.Errorf("fetch spk for device %s: %w", dev.ID, err)
			}

			b := DeviceKeyBundle{
				Device:       dev,
				SignedPreKey: spk,
			}

			var opk OneTimePreKey
			err = tx.QueryRow(ctx,
				`DELETE FROM e2ee_one_time_pre_keys
				 WHERE id = (
				     SELECT id FROM e2ee_one_time_pre_keys
				     WHERE device_id = $1
				     ORDER BY key_id
				     LIMIT 1
				     FOR UPDATE SKIP LOCKED
				 )
				 RETURNING id, device_id, key_id, public_key, created_at`, dev.ID,
			).Scan(&opk.ID, &opk.DeviceID, &opk.KeyID, &opk.PublicKey, &opk.CreatedAt)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("consume opk for device %s: %w", dev.ID, err)
			}
			if err == nil {
				b.OneTimePreKey = &opk
			}

			bundles = append(bundles, b)
		}

		bundle = &UserKeyBundle{
			UserID:  targetUserID,
			Devices: bundles,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return bundle, nil
}

// StoreMessageKeys stores per-device encrypted message keys using CopyFrom.
func (r *PGRepository) StoreMessageKeys(ctx context.Context, messageID uuid.UUID, keys map[uuid.UUID][]byte) error {
	if len(keys) == 0 {
		return nil
	}

	rows := make([][]any, 0, len(keys))
	for deviceRowID, encKey := range keys {
		rows = append(rows, []any{messageID, deviceRowID, encKey})
	}

	_, err := r.db.CopyFrom(
		ctx,
		pgx.Identifier{"dm_message_keys"},
		[]string{"message_id", "device_id", "encrypted_key"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copy message keys: %w", err)
	}
	return nil
}

// GetMessageKeyForDevice returns the encrypted key for a single message and device.
func (r *PGRepository) GetMessageKeyForDevice(ctx context.Context, messageID, deviceRowID uuid.UUID) ([]byte, error) {
	var key []byte
	err := r.db.QueryRow(ctx,
		`SELECT encrypted_key FROM dm_message_keys WHERE message_id = $1 AND device_id = $2`,
		messageID, deviceRowID,
	).Scan(&key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get message key: %w", err)
	}
	return key, nil
}

// GetMessageKeysBatch returns encrypted keys for multiple messages for a single device.
func (r *PGRepository) GetMessageKeysBatch(ctx context.Context, messageIDs []uuid.UUID, deviceRowID uuid.UUID) (map[uuid.UUID][]byte, error) {
	if len(messageIDs) == 0 {
		return map[uuid.UUID][]byte{}, nil
	}

	rows, err := r.db.Query(ctx,
		`SELECT message_id, encrypted_key FROM dm_message_keys
		 WHERE message_id = ANY($1) AND device_id = $2`, messageIDs, deviceRowID)
	if err != nil {
		return nil, fmt.Errorf("batch get message keys: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID][]byte, len(messageIDs))
	for rows.Next() {
		var msgID uuid.UUID
		var key []byte
		if err := rows.Scan(&msgID, &key); err != nil {
			return nil, fmt.Errorf("scan message key: %w", err)
		}
		result[msgID] = key
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("batch get message keys: %w", err)
	}
	return result, nil
}
