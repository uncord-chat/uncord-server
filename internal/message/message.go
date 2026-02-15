package message

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Sentinel errors for the message package.
var (
	ErrNotFound       = errors.New("message not found")
	ErrContentTooLong = errors.New("message content exceeds the maximum length")
	ErrEmptyContent   = errors.New("message content must not be empty")
	ErrReplyNotFound  = errors.New("reply target message not found")
	ErrNotAuthor      = errors.New("you can only modify your own messages")
	ErrAlreadyDeleted = errors.New("message has already been deleted")
)

// Pagination defaults.
const (
	DefaultLimit = 50
	MaxLimit     = 100
)

// Message holds the fields read from the database, including joined author information.
type Message struct {
	ID        uuid.UUID
	ChannelID uuid.UUID
	AuthorID  uuid.UUID
	Content   string
	EditedAt  *time.Time
	ReplyToID *uuid.UUID
	Pinned    bool
	Deleted   bool
	CreatedAt time.Time
	UpdatedAt time.Time

	// Author fields joined from the users table.
	AuthorUsername    string
	AuthorDisplayName *string
	AuthorAvatarKey   *string
}

// CreateParams groups the inputs for creating a new message.
type CreateParams struct {
	ChannelID uuid.UUID
	AuthorID  uuid.UUID
	Content   string
	ReplyToID *uuid.UUID
}

// ValidateContent checks that content is non-empty after trimming and does not exceed the given maximum rune count.
func ValidateContent(content string, maxLength int) (string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", ErrEmptyContent
	}
	if utf8.RuneCountInString(trimmed) > maxLength {
		return "", ErrContentTooLong
	}
	return trimmed, nil
}

// ClampLimit constrains a requested page size to [1, MaxLimit], defaulting to DefaultLimit when the input is zero or
// negative.
func ClampLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// Repository defines the data-access contract for message operations.
type Repository interface {
	Create(ctx context.Context, params CreateParams) (*Message, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Message, error)
	List(ctx context.Context, channelID uuid.UUID, before *uuid.UUID, limit int) ([]Message, error)
	Update(ctx context.Context, id uuid.UUID, content string) (*Message, error)
	SoftDelete(ctx context.Context, id uuid.UUID) error
}
