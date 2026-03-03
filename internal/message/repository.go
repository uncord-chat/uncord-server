package message

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uncord-chat/uncord-server/internal/postgres"
)

const selectColumns = `m.id, m.channel_id, m.author_id, m.content, m.edited_at, m.reply_to_id, m.thread_id,
m.pinned, m.deleted_at, m.deleted_by, m.encrypted, m.created_at, m.updated_at,
u.username, u.display_name, u.avatar_key`

const baseJoin = "FROM messages m JOIN users u ON u.id = m.author_id"

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed message repository.
func NewPGRepository(db *pgxpool.Pool) *PGRepository {
	return &PGRepository{db: db}
}

// Create inserts a new message and returns it with joined author information. When reply_to_id is set, the referenced
// message must exist, be in the same channel, and not be deleted.
func (r *PGRepository) Create(ctx context.Context, params CreateParams) (*Message, error) {
	var msg Message
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if params.ReplyToID != nil {
			var exists bool
			err := tx.QueryRow(ctx,
				"SELECT EXISTS(SELECT 1 FROM messages WHERE id = $1 AND channel_id = $2 AND deleted_at IS NULL)",
				*params.ReplyToID, params.ChannelID,
			).Scan(&exists)
			if err != nil {
				return fmt.Errorf("check reply target: %w", err)
			}
			if !exists {
				return ErrReplyNotFound
			}
		}

		row := tx.QueryRow(ctx,
			`INSERT INTO messages (channel_id, author_id, content, reply_to_id, thread_id, encrypted)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 RETURNING id, created_at, updated_at`,
			params.ChannelID, params.AuthorID, params.Content, params.ReplyToID, params.ThreadID, params.Encrypted,
		)

		msg.ChannelID = params.ChannelID
		msg.AuthorID = params.AuthorID
		msg.Content = params.Content
		msg.ReplyToID = params.ReplyToID
		msg.ThreadID = params.ThreadID
		msg.Encrypted = params.Encrypted
		if err := row.Scan(&msg.ID, &msg.CreatedAt, &msg.UpdatedAt); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}

		// Fetch author info within the same transaction for consistency.
		err := tx.QueryRow(ctx,
			"SELECT username, display_name, avatar_key FROM users WHERE id = $1",
			params.AuthorID,
		).Scan(&msg.AuthorUsername, &msg.AuthorDisplayName, &msg.AuthorAvatarKey)
		if err != nil {
			return fmt.Errorf("fetch author info: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// GetByID returns a single non-deleted message by ID with joined author information.
func (r *PGRepository) GetByID(ctx context.Context, id uuid.UUID) (*Message, error) {
	row := r.db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s %s WHERE m.id = $1 AND m.deleted_at IS NULL", selectColumns, baseJoin), id,
	)
	msg, err := scanMessage(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query message by id: %w", err)
	}
	return msg, nil
}

// List returns non-deleted messages in a channel ordered newest first. When before is non-nil, only messages created
// before the referenced message are returned (cursor-based pagination).
func (r *PGRepository) List(ctx context.Context, channelID uuid.UUID, before *uuid.UUID, limit int) ([]Message, error) {
	var rows pgx.Rows
	var err error

	if before != nil {
		rows, err = r.db.Query(ctx, fmt.Sprintf(
			`SELECT %s %s
			 WHERE m.channel_id = $1 AND m.deleted_at IS NULL
			   AND (m.created_at, m.id) < (SELECT created_at, id FROM messages WHERE id = $2)
			 ORDER BY m.created_at DESC, m.id DESC
			 LIMIT $3`, selectColumns, baseJoin),
			channelID, *before, limit,
		)
	} else {
		rows, err = r.db.Query(ctx, fmt.Sprintf(
			`SELECT %s %s
			 WHERE m.channel_id = $1 AND m.deleted_at IS NULL
			 ORDER BY m.created_at DESC, m.id DESC
			 LIMIT $2`, selectColumns, baseJoin),
			channelID, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, *msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return messages, nil
}

// Update sets new content on a non-deleted message and marks it as edited. Returns the updated message with joined
// author information.
func (r *PGRepository) Update(ctx context.Context, id uuid.UUID, content string) (*Message, error) {
	row := r.db.QueryRow(ctx,
		`UPDATE messages SET content = $1, edited_at = NOW()
		 WHERE id = $2 AND deleted_at IS NULL
		 RETURNING id`, content, id,
	)

	var updatedID uuid.UUID
	if err := row.Scan(&updatedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update message: %w", err)
	}

	return r.GetByID(ctx, updatedID)
}

// SoftDelete marks a message as deleted by recording the deletion timestamp and the actor who performed it. Returns
// ErrNotFound if the message does not exist or is already deleted.
func (r *PGRepository) SoftDelete(ctx context.Context, id uuid.UUID, deletedBy uuid.UUID) error {
	tag, err := r.db.Exec(ctx,
		"UPDATE messages SET deleted_at = NOW(), deleted_by = $2 WHERE id = $1 AND deleted_at IS NULL", id, deletedBy,
	)
	if err != nil {
		return fmt.Errorf("soft delete message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Pin marks a message as pinned. Returns ErrAlreadyPinned if the message is already pinned, or ErrNotFound if the
// message does not exist or is deleted.
func (r *PGRepository) Pin(ctx context.Context, id uuid.UUID) (*Message, error) {
	tag, err := r.db.Exec(ctx,
		"UPDATE messages SET pinned = true WHERE id = $1 AND deleted_at IS NULL AND pinned = false", id,
	)
	if err != nil {
		return nil, fmt.Errorf("pin message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Distinguish "not found / deleted" from "already pinned" by checking whether the message exists.
		msg, getErr := r.GetByID(ctx, id)
		if getErr != nil {
			return nil, ErrNotFound
		}
		if msg.Pinned {
			return nil, ErrAlreadyPinned
		}
		return nil, ErrNotFound
	}
	return r.GetByID(ctx, id)
}

// Unpin marks a message as unpinned. Returns ErrNotPinned if the message is not currently pinned, or ErrNotFound if the
// message does not exist or is deleted.
func (r *PGRepository) Unpin(ctx context.Context, id uuid.UUID) (*Message, error) {
	tag, err := r.db.Exec(ctx,
		"UPDATE messages SET pinned = false WHERE id = $1 AND deleted_at IS NULL AND pinned = true", id,
	)
	if err != nil {
		return nil, fmt.Errorf("unpin message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Distinguish "not found / deleted" from "not pinned" by checking whether the message exists.
		msg, getErr := r.GetByID(ctx, id)
		if getErr != nil {
			return nil, ErrNotFound
		}
		if !msg.Pinned {
			return nil, ErrNotPinned
		}
		return nil, ErrNotFound
	}
	return r.GetByID(ctx, id)
}

// ListPinned returns all pinned, non-deleted messages in a channel ordered newest first.
func (r *PGRepository) ListPinned(ctx context.Context, channelID uuid.UUID) ([]Message, error) {
	rows, err := r.db.Query(ctx, fmt.Sprintf(
		`SELECT %s %s
		 WHERE m.channel_id = $1 AND m.pinned = true AND m.deleted_at IS NULL
		 ORDER BY m.created_at DESC
		 LIMIT 50`, selectColumns, baseJoin),
		channelID,
	)
	if err != nil {
		return nil, fmt.Errorf("query pinned messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		msg, scanErr := scanMessage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		messages = append(messages, *msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pinned messages: %w", err)
	}
	return messages, nil
}

// ListByThread returns non-deleted messages in a thread ordered newest first. Cursor-based pagination is supported via
// the before parameter, matching the List method's behaviour.
func (r *PGRepository) ListByThread(ctx context.Context, threadID uuid.UUID, before *uuid.UUID, limit int) ([]Message, error) {
	var rows pgx.Rows
	var err error

	if before != nil {
		rows, err = r.db.Query(ctx, fmt.Sprintf(
			`SELECT %s %s
			 WHERE m.thread_id = $1 AND m.deleted_at IS NULL
			   AND (m.created_at, m.id) < (SELECT created_at, id FROM messages WHERE id = $2)
			 ORDER BY m.created_at DESC, m.id DESC
			 LIMIT $3`, selectColumns, baseJoin),
			threadID, *before, limit,
		)
	} else {
		rows, err = r.db.Query(ctx, fmt.Sprintf(
			`SELECT %s %s
			 WHERE m.thread_id = $1 AND m.deleted_at IS NULL
			 ORDER BY m.created_at DESC, m.id DESC
			 LIMIT $2`, selectColumns, baseJoin),
			threadID, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query thread messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		msg, scanErr := scanMessage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		messages = append(messages, *msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread messages: %w", err)
	}
	return messages, nil
}

// scanMessage scans a single row into a Message struct.
func scanMessage(row pgx.Row) (*Message, error) {
	var msg Message
	err := row.Scan(
		&msg.ID, &msg.ChannelID, &msg.AuthorID, &msg.Content, &msg.EditedAt, &msg.ReplyToID, &msg.ThreadID,
		&msg.Pinned, &msg.DeletedAt, &msg.DeletedBy, &msg.Encrypted, &msg.CreatedAt, &msg.UpdatedAt,
		&msg.AuthorUsername, &msg.AuthorDisplayName, &msg.AuthorAvatarKey,
	)
	if err != nil {
		return nil, fmt.Errorf("scan message: %w", err)
	}
	return &msg, nil
}
