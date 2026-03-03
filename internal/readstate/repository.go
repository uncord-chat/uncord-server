package readstate

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed read state repository.
func NewPGRepository(db *pgxpool.Pool) *PGRepository {
	return &PGRepository{db: db}
}

// ListByUser returns all read states for the given user.
func (r *PGRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]ReadState, error) {
	rows, err := r.db.Query(ctx,
		`SELECT user_id, channel_id, last_message_id, mention_count, updated_at
		 FROM channel_read_states WHERE user_id = $1`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query read states: %w", err)
	}
	defer rows.Close()

	var states []ReadState
	for rows.Next() {
		var rs ReadState
		if err := rows.Scan(&rs.UserID, &rs.ChannelID, &rs.LastMessageID, &rs.MentionCount, &rs.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan read state: %w", err)
		}
		states = append(states, rs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate read states: %w", err)
	}
	return states, nil
}

// Ack upserts a read state for the given user and channel, advancing the read position to the specified message. The
// read position only moves forward based on message created_at timestamps. If the message does not exist in the
// channel (or is deleted), ErrMessageNotInChannel is returned.
func (r *PGRepository) Ack(ctx context.Context, userID, channelID, messageID uuid.UUID) (*ReadState, error) {
	// The CTE validates that the target message exists in the channel and is not deleted, then conditionally upserts
	// the read position. The forward-only guard compares created_at timestamps because UUIDs v4 are not time-ordered.
	const query = `
		WITH new_msg AS (
			SELECT id, created_at FROM messages WHERE id = $3 AND channel_id = $2 AND deleted_at IS NULL
		),
		current AS (
			SELECT last_message_id FROM channel_read_states WHERE user_id = $1 AND channel_id = $2
		),
		old_msg AS (
			SELECT created_at FROM messages WHERE id = (SELECT last_message_id FROM current)
		)
		INSERT INTO channel_read_states (user_id, channel_id, last_message_id, mention_count, updated_at)
		SELECT $1, $2, new_msg.id, 0, NOW()
		FROM new_msg
		WHERE EXISTS (SELECT 1 FROM new_msg)
		ON CONFLICT (user_id, channel_id) DO UPDATE
		SET last_message_id = CASE
				WHEN channel_read_states.last_message_id IS NULL THEN (SELECT id FROM new_msg)
				WHEN (SELECT created_at FROM new_msg) > COALESCE((SELECT created_at FROM old_msg), '-infinity'::timestamptz)
					THEN (SELECT id FROM new_msg)
				ELSE channel_read_states.last_message_id
			END,
			mention_count = 0,
			updated_at = NOW()
		RETURNING user_id, channel_id, last_message_id, mention_count, updated_at`

	var rs ReadState
	err := r.db.QueryRow(ctx, query, userID, channelID, messageID).
		Scan(&rs.UserID, &rs.ChannelID, &rs.LastMessageID, &rs.MentionCount, &rs.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMessageNotInChannel
		}
		return nil, fmt.Errorf("ack read state: %w", err)
	}
	return &rs, nil
}

// DeleteByChannel removes all read states for the given channel. This is intended for use when a channel is deleted.
func (r *PGRepository) DeleteByChannel(ctx context.Context, channelID uuid.UUID) error {
	_, err := r.db.Exec(ctx, "DELETE FROM channel_read_states WHERE channel_id = $1", channelID)
	if err != nil {
		return fmt.Errorf("delete read states by channel: %w", err)
	}
	return nil
}
