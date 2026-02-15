package attachment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const selectColumns = `id, message_id, channel_id, uploader_id, filename, content_type,
size_bytes, storage_key, width, height, thumbnail_key, created_at`

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed attachment repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// Create inserts a new pending attachment record with message_id NULL.
func (r *PGRepository) Create(ctx context.Context, params CreateParams) (*Attachment, error) {
	row := r.db.QueryRow(ctx,
		`INSERT INTO message_attachments (channel_id, uploader_id, filename, content_type, size_bytes, storage_key, width, height)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING `+selectColumns,
		params.ChannelID, params.UploaderID, params.Filename, params.ContentType,
		params.SizeBytes, params.StorageKey, params.Width, params.Height,
	)
	return scanAttachment(row)
}

// GetByID returns a single attachment by ID.
func (r *PGRepository) GetByID(ctx context.Context, id uuid.UUID) (*Attachment, error) {
	row := r.db.QueryRow(ctx,
		"SELECT "+selectColumns+" FROM message_attachments WHERE id = $1", id,
	)
	a, err := scanAttachment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query attachment by id: %w", err)
	}
	return a, nil
}

// LinkToMessage atomically assigns the given attachment IDs to a message. Only pending attachments owned by uploaderID
// are linked. Returns ErrNotFound if the number of updated rows does not match the number of requested IDs.
func (r *PGRepository) LinkToMessage(ctx context.Context, attachmentIDs []uuid.UUID, messageID uuid.UUID, uploaderID uuid.UUID) ([]Attachment, error) {
	rows, err := r.db.Query(ctx,
		`UPDATE message_attachments
		 SET message_id = $1
		 WHERE id = ANY($2) AND uploader_id = $3 AND message_id IS NULL
		 RETURNING `+selectColumns,
		messageID, attachmentIDs, uploaderID,
	)
	if err != nil {
		return nil, fmt.Errorf("link attachments to message: %w", err)
	}
	defer rows.Close()

	var result []Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan linked attachment: %w", err)
		}
		result = append(result, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate linked attachments: %w", err)
	}

	if len(result) != len(attachmentIDs) {
		return nil, ErrNotFound
	}
	return result, nil
}

// ListByMessage returns all attachments linked to the given message.
func (r *PGRepository) ListByMessage(ctx context.Context, messageID uuid.UUID) ([]Attachment, error) {
	rows, err := r.db.Query(ctx,
		"SELECT "+selectColumns+" FROM message_attachments WHERE message_id = $1 ORDER BY created_at",
		messageID,
	)
	if err != nil {
		return nil, fmt.Errorf("query attachments by message: %w", err)
	}
	defer rows.Close()
	return collectAttachments(rows)
}

// ListByMessages returns attachments for multiple messages in a single query, keyed by message ID.
func (r *PGRepository) ListByMessages(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]Attachment, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}

	rows, err := r.db.Query(ctx,
		"SELECT "+selectColumns+" FROM message_attachments WHERE message_id = ANY($1) ORDER BY created_at",
		messageIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("query attachments by messages: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID][]Attachment, len(messageIDs))
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		if a.MessageID != nil {
			result[*a.MessageID] = append(result[*a.MessageID], *a)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachments: %w", err)
	}
	return result, nil
}

// SetThumbnailKey records the storage key of a generated thumbnail for the given attachment.
func (r *PGRepository) SetThumbnailKey(ctx context.Context, id uuid.UUID, thumbnailKey string) error {
	tag, err := r.db.Exec(ctx,
		"UPDATE message_attachments SET thumbnail_key = $1 WHERE id = $2",
		thumbnailKey, id,
	)
	if err != nil {
		return fmt.Errorf("set thumbnail key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeOrphans deletes pending attachments older than the given threshold and returns their storage keys (including
// thumbnail keys) for file cleanup.
func (r *PGRepository) PurgeOrphans(ctx context.Context, olderThan time.Time) ([]string, error) {
	rows, err := r.db.Query(ctx,
		`DELETE FROM message_attachments
		 WHERE message_id IS NULL AND created_at < $1
		 RETURNING storage_key, thumbnail_key`,
		olderThan,
	)
	if err != nil {
		return nil, fmt.Errorf("purge orphan attachments: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var storageKey string
		var thumbnailKey *string
		if err := rows.Scan(&storageKey, &thumbnailKey); err != nil {
			return nil, fmt.Errorf("scan orphan key: %w", err)
		}
		keys = append(keys, storageKey)
		if thumbnailKey != nil {
			keys = append(keys, *thumbnailKey)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orphan keys: %w", err)
	}
	return keys, nil
}

func scanAttachment(row pgx.Row) (*Attachment, error) {
	var a Attachment
	err := row.Scan(
		&a.ID, &a.MessageID, &a.ChannelID, &a.UploaderID, &a.Filename, &a.ContentType,
		&a.SizeBytes, &a.StorageKey, &a.Width, &a.Height, &a.ThumbnailKey, &a.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func collectAttachments(rows pgx.Rows) ([]Attachment, error) {
	var result []Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		result = append(result, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachments: %w", err)
	}
	return result, nil
}
