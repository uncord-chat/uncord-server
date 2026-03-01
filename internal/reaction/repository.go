package reaction

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-server/internal/postgres"
)

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed reaction repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// Add inserts a new reaction. Returns ErrAlreadyReacted on unique constraint violation.
func (r *PGRepository) Add(ctx context.Context, messageID, userID uuid.UUID, emojiID *uuid.UUID, emojiUnicode *string) (*Reaction, error) {
	row := r.db.QueryRow(ctx,
		`INSERT INTO reactions (message_id, user_id, emoji_id, emoji_unicode)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, message_id, user_id, emoji_id, emoji_unicode, created_at`,
		messageID, userID, emojiID, emojiUnicode,
	)
	var rxn Reaction
	err := row.Scan(&rxn.ID, &rxn.MessageID, &rxn.UserID, &rxn.EmojiID, &rxn.EmojiUnicode, &rxn.CreatedAt)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, ErrAlreadyReacted
		}
		return nil, fmt.Errorf("add reaction: %w", err)
	}
	return &rxn, nil
}

// Remove deletes a reaction matching the given message, user, and emoji. Returns ErrNotFound when no row is deleted.
func (r *PGRepository) Remove(ctx context.Context, messageID, userID uuid.UUID, emojiID *uuid.UUID, emojiUnicode *string) error {
	var tag pgconn.CommandTag
	var err error

	if emojiID != nil {
		tag, err = r.db.Exec(ctx,
			`DELETE FROM reactions WHERE message_id = $1 AND user_id = $2 AND emoji_id = $3`,
			messageID, userID, emojiID,
		)
	} else {
		tag, err = r.db.Exec(ctx,
			`DELETE FROM reactions WHERE message_id = $1 AND user_id = $2 AND emoji_unicode = $3`,
			messageID, userID, emojiUnicode,
		)
	}
	if err != nil {
		return fmt.Errorf("remove reaction: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByMessage returns all reactions on a message with joined username, ordered by creation time.
func (r *PGRepository) ListByMessage(ctx context.Context, messageID uuid.UUID) ([]Reaction, error) {
	rows, err := r.db.Query(ctx,
		`SELECT r.id, r.message_id, r.user_id, r.emoji_id, r.emoji_unicode, r.created_at, u.username
		 FROM reactions r
		 JOIN users u ON u.id = r.user_id
		 WHERE r.message_id = $1
		 ORDER BY r.created_at`,
		messageID,
	)
	if err != nil {
		return nil, fmt.Errorf("list reactions by message: %w", err)
	}
	defer rows.Close()
	return collectReactions(rows)
}

// ListByEmoji returns all reactions on a message for a specific emoji, ordered by creation time.
func (r *PGRepository) ListByEmoji(ctx context.Context, messageID uuid.UUID, emojiID *uuid.UUID, emojiUnicode *string) ([]Reaction, error) {
	var rows pgx.Rows
	var err error

	if emojiID != nil {
		rows, err = r.db.Query(ctx,
			`SELECT r.id, r.message_id, r.user_id, r.emoji_id, r.emoji_unicode, r.created_at, u.username
			 FROM reactions r
			 JOIN users u ON u.id = r.user_id
			 WHERE r.message_id = $1 AND r.emoji_id = $2
			 ORDER BY r.created_at`,
			messageID, emojiID,
		)
	} else {
		rows, err = r.db.Query(ctx,
			`SELECT r.id, r.message_id, r.user_id, r.emoji_id, r.emoji_unicode, r.created_at, u.username
			 FROM reactions r
			 JOIN users u ON u.id = r.user_id
			 WHERE r.message_id = $1 AND r.emoji_unicode = $2
			 ORDER BY r.created_at`,
			messageID, emojiUnicode,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("list reactions by emoji: %w", err)
	}
	defer rows.Close()
	return collectReactions(rows)
}

// SummariesByMessages returns grouped reaction counts for multiple messages in a single query. The result is keyed by
// message ID with each value containing the distinct emoji counts for that message.
func (r *PGRepository) SummariesByMessages(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]Summary, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}

	rows, err := r.db.Query(ctx,
		`SELECT message_id, emoji_id, emoji_unicode, COUNT(*) AS cnt
		 FROM reactions
		 WHERE message_id = ANY($1)
		 GROUP BY message_id, emoji_id, emoji_unicode
		 ORDER BY message_id, MIN(created_at)`,
		messageIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("summarise reactions: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID][]Summary, len(messageIDs))
	for rows.Next() {
		var msgID uuid.UUID
		var s Summary
		if err := rows.Scan(&msgID, &s.EmojiID, &s.EmojiUnicode, &s.Count); err != nil {
			return nil, fmt.Errorf("scan reaction summary: %w", err)
		}
		result[msgID] = append(result[msgID], s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reaction summaries: %w", err)
	}
	return result, nil
}

// UserReactionsByMessages returns the set of emoji identifiers the given user has reacted with on each message. The
// outer map is keyed by message ID; the inner map keys are "custom:{uuid}" for custom emoji or the unicode string for
// standard emoji.
func (r *PGRepository) UserReactionsByMessages(ctx context.Context, messageIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID]map[string]bool, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}

	rows, err := r.db.Query(ctx,
		`SELECT message_id, emoji_id, emoji_unicode
		 FROM reactions
		 WHERE message_id = ANY($1) AND user_id = $2`,
		messageIDs, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query user reactions: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]map[string]bool)
	for rows.Next() {
		var msgID uuid.UUID
		var emojiID *uuid.UUID
		var emojiUnicode *string
		if err := rows.Scan(&msgID, &emojiID, &emojiUnicode); err != nil {
			return nil, fmt.Errorf("scan user reaction: %w", err)
		}
		if result[msgID] == nil {
			result[msgID] = make(map[string]bool)
		}
		if emojiID != nil {
			result[msgID]["custom:"+emojiID.String()] = true
		} else if emojiUnicode != nil {
			result[msgID][*emojiUnicode] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user reactions: %w", err)
	}
	return result, nil
}

func collectReactions(rows pgx.Rows) ([]Reaction, error) {
	var result []Reaction
	for rows.Next() {
		var rxn Reaction
		if err := rows.Scan(&rxn.ID, &rxn.MessageID, &rxn.UserID, &rxn.EmojiID, &rxn.EmojiUnicode, &rxn.CreatedAt, &rxn.Username); err != nil {
			return nil, fmt.Errorf("scan reaction: %w", err)
		}
		result = append(result, rxn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reactions: %w", err)
	}
	return result, nil
}
