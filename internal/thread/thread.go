package thread

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Sentinel errors for the thread package.
var (
	ErrNotFound        = errors.New("thread not found")
	ErrAlreadyExists   = errors.New("a thread already exists for this message")
	ErrArchived        = errors.New("thread is archived")
	ErrLocked          = errors.New("thread is locked")
	ErrNameLength      = errors.New("thread name must be between 1 and 100 characters")
	ErrMessageNotFound = errors.New("parent message not found")
)

// Thread holds the fields read from the database.
type Thread struct {
	ID              uuid.UUID
	ChannelID       uuid.UUID
	ParentMessageID uuid.UUID
	Name            string
	Archived        bool
	Locked          bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CreateParams groups the inputs for creating a new thread.
type CreateParams struct {
	ChannelID       uuid.UUID
	ParentMessageID uuid.UUID
	Name            string
}

// UpdateParams groups the optional fields for updating a thread.
type UpdateParams struct {
	Name     *string
	Archived *bool
	Locked   *bool
}

// ValidateNameRequired validates and trims a name that must be present. It returns the trimmed result on success.
func ValidateNameRequired(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 100 {
		return "", ErrNameLength
	}
	return trimmed, nil
}

// ValidateName checks that a non-nil name is between 1 and 100 characters (runes) after trimming whitespace. A nil
// pointer means "no change" (useful for PATCH semantics); a non-nil pointer is always validated. On success the
// pointed-to value is replaced with the trimmed result.
func ValidateName(name *string) error {
	if name == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*name)
	if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 100 {
		return ErrNameLength
	}
	*name = trimmed
	return nil
}

// Repository defines the data-access contract for thread operations.
type Repository interface {
	Create(ctx context.Context, params CreateParams) (*Thread, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Thread, error)
	ListByChannel(ctx context.Context, channelID uuid.UUID) ([]Thread, error)
	Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*Thread, error)
}

var _ Repository = (*PGRepository)(nil)
