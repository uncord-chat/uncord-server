package reaction

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for the reaction package.
var (
	ErrNotFound       = errors.New("reaction not found")
	ErrAlreadyReacted = errors.New("user has already reacted with this emoji")
)

// Reaction holds the fields read from the database for a message reaction.
type Reaction struct {
	ID           uuid.UUID
	MessageID    uuid.UUID
	UserID       uuid.UUID
	EmojiID      *uuid.UUID
	EmojiUnicode *string
	CreatedAt    time.Time

	// Username is joined from the users table for list endpoints.
	Username string
}

// Summary is a grouped count of a single emoji on a message, used for batch loading into message responses.
type Summary struct {
	EmojiID      *uuid.UUID
	EmojiUnicode *string
	Count        int
}

// Repository defines the data-access contract for reaction operations.
type Repository interface {
	// Add inserts a new reaction. Returns ErrAlreadyReacted if the user already reacted with this emoji on this
	// message.
	Add(ctx context.Context, messageID, userID uuid.UUID, emojiID *uuid.UUID, emojiUnicode *string) (*Reaction, error)

	// Remove deletes a reaction. Returns ErrNotFound if no matching reaction exists.
	Remove(ctx context.Context, messageID, userID uuid.UUID, emojiID *uuid.UUID, emojiUnicode *string) error

	// ListByMessage returns all reactions on a message, ordered by creation time.
	ListByMessage(ctx context.Context, messageID uuid.UUID) ([]Reaction, error)

	// ListByEmoji returns all reactions on a message for a specific emoji, ordered by creation time.
	ListByEmoji(ctx context.Context, messageID uuid.UUID, emojiID *uuid.UUID, emojiUnicode *string) ([]Reaction, error)

	// SummariesByMessages returns grouped reaction counts for multiple messages in a single query, keyed by message
	// ID. The caller populates the Me boolean by cross-referencing with the requesting user's ID.
	SummariesByMessages(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]Summary, error)

	// UserReactionsByMessages returns the set of emoji identifiers the given user has reacted with on each message.
	// The outer map is keyed by message ID; the inner map keys are "custom:{uuid}" for custom emoji or the unicode
	// string for standard emoji.
	UserReactionsByMessages(ctx context.Context, messageIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID]map[string]bool, error)
}
