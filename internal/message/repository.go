package message

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const selectColumns = `m.id, m.channel_id, m.author_id, m.content, m.edited_at, m.reply_to_id,
m.pinned, m.deleted, m.created_at, m.updated_at,
u.username, u.display_name, u.avatar_key`

const baseJoin = "FROM messages m JOIN users u ON u.id = m.author_id"

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed message repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// Create inserts a new message and returns it with joined author information. When reply_to_id is set, the referenced
// message must exist, be in the same channel, and not be deleted.
func (r *PGRepository) Create(ctx context.Context, params CreateParams) (*Message, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create message tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			r.log.Warn().Err(err).Msg("tx rollback failed")
		}
	}()

	if params.ReplyToID != nil {
		var exists bool
		err := tx.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM messages WHERE id = $1 AND channel_id = $2 AND deleted = false)",
			*params.ReplyToID, params.ChannelID,
		).Scan(&exists)
		if err != nil {
			return nil, fmt.Errorf("check reply target: %w", err)
		}
		if !exists {
			return nil, ErrReplyNotFound
		}
	}

	row := tx.QueryRow(ctx,
		`INSERT INTO messages (channel_id, author_id, content, reply_to_id)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at, updated_at`,
		params.ChannelID, params.AuthorID, params.Content, params.ReplyToID,
	)

	var msg Message
	msg.ChannelID = params.ChannelID
	msg.AuthorID = params.AuthorID
	msg.Content = params.Content
	msg.ReplyToID = params.ReplyToID
	if err := row.Scan(&msg.ID, &msg.CreatedAt, &msg.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}

	// Fetch author info within the same transaction for consistency.
	err = tx.QueryRow(ctx,
		"SELECT username, display_name, avatar_key FROM users WHERE id = $1",
		params.AuthorID,
	).Scan(&msg.AuthorUsername, &msg.AuthorDisplayName, &msg.AuthorAvatarKey)
	if err != nil {
		return nil, fmt.Errorf("fetch author info: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create message tx: %w", err)
	}
	return &msg, nil
}

// GetByID returns a single non-deleted message by ID with joined author information.
func (r *PGRepository) GetByID(ctx context.Context, id uuid.UUID) (*Message, error) {
	row := r.db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s %s WHERE m.id = $1 AND m.deleted = false", selectColumns, baseJoin), id,
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
			 WHERE m.channel_id = $1 AND m.deleted = false
			   AND (m.created_at, m.id) < (SELECT created_at, id FROM messages WHERE id = $2)
			 ORDER BY m.created_at DESC, m.id DESC
			 LIMIT $3`, selectColumns, baseJoin),
			channelID, *before, limit,
		)
	} else {
		rows, err = r.db.Query(ctx, fmt.Sprintf(
			`SELECT %s %s
			 WHERE m.channel_id = $1 AND m.deleted = false
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
			return nil, fmt.Errorf("scan message: %w", err)
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
		 WHERE id = $2 AND deleted = false
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

// SoftDelete marks a message as deleted. Returns ErrNotFound if the message does not exist or is already deleted.
func (r *PGRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx,
		"UPDATE messages SET deleted = true WHERE id = $1 AND deleted = false", id,
	)
	if err != nil {
		return fmt.Errorf("soft delete message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanMessage scans a single row into a Message struct.
func scanMessage(row pgx.Row) (*Message, error) {
	var msg Message
	err := row.Scan(
		&msg.ID, &msg.ChannelID, &msg.AuthorID, &msg.Content, &msg.EditedAt, &msg.ReplyToID,
		&msg.Pinned, &msg.Deleted, &msg.CreatedAt, &msg.UpdatedAt,
		&msg.AuthorUsername, &msg.AuthorDisplayName, &msg.AuthorAvatarKey,
	)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}
