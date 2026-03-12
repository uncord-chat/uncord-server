package settingsync

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const selectColumns = "user_id, encrypted_blob, salt, nonce, blob_version, updated_at"

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed synced settings repository.
func NewPGRepository(db *pgxpool.Pool) *PGRepository {
	return &PGRepository{db: db}
}

// Get returns the encrypted synced settings blob for the given user.
func (r *PGRepository) Get(ctx context.Context, userID uuid.UUID) (*Blob, error) {
	row := r.db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s FROM user_synced_settings WHERE user_id = $1", selectColumns), userID)
	b, err := scanBlob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query synced settings: %w", err)
	}
	return b, nil
}

// Upsert inserts or updates the encrypted synced settings blob for the given user. The caller must ensure that
// params.BlobVersion is higher than any previously stored version; this method enforces that constraint via a
// conditional UPDATE that only applies when the new version exceeds the current one. If the version check fails on
// update, ErrVersionConflict is returned.
func (r *PGRepository) Upsert(ctx context.Context, userID uuid.UUID, params UpsertParams) (*Blob, error) {
	const query = `INSERT INTO user_synced_settings (user_id, encrypted_blob, salt, nonce, blob_version)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			encrypted_blob = EXCLUDED.encrypted_blob,
			salt           = EXCLUDED.salt,
			nonce          = EXCLUDED.nonce,
			blob_version   = EXCLUDED.blob_version
		WHERE EXCLUDED.blob_version > user_synced_settings.blob_version
		RETURNING ` + selectColumns

	row := r.db.QueryRow(ctx, query,
		userID, params.EncryptedBlob, params.Salt, params.Nonce, params.BlobVersion)
	b, err := scanBlob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVersionConflict
		}
		return nil, fmt.Errorf("upsert synced settings: %w", err)
	}
	return b, nil
}

// Delete removes the encrypted synced settings blob for the given user.
func (r *PGRepository) Delete(ctx context.Context, userID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM user_synced_settings WHERE user_id = $1", userID)
	if err != nil {
		return fmt.Errorf("delete synced settings: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanBlob scans a single row into a Blob struct.
func scanBlob(row pgx.Row) (*Blob, error) {
	var b Blob
	err := row.Scan(&b.UserID, &b.EncryptedBlob, &b.Salt, &b.Nonce, &b.BlobVersion, &b.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan synced settings blob: %w", err)
	}
	return &b, nil
}
