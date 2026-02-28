package emoji

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/uncord-chat/uncord-protocol/models"
)

// Sentinel errors for the emoji package.
var (
	ErrNotFound     = errors.New("custom emoji not found")
	ErrNameTaken    = errors.New("emoji name is already in use")
	ErrLimitReached = errors.New("maximum number of custom emoji reached")
	ErrInvalidName  = errors.New("emoji name must be 2-32 alphanumeric or underscore characters")
)

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{2,32}$`)

// Emoji holds the fields read from the database for a custom emoji.
type Emoji struct {
	ID         uuid.UUID
	Name       string
	Animated   bool
	StorageKey string
	UploaderID uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ToModel converts the internal Emoji to the protocol response type. The caller provides the full public URL derived
// from the storage key.
func (e *Emoji) ToModel(url string) models.Emoji {
	return models.Emoji{
		ID:         e.ID.String(),
		Name:       e.Name,
		Animated:   e.Animated,
		URL:        url,
		UploaderID: e.UploaderID.String(),
		CreatedAt:  e.CreatedAt.Format(time.RFC3339),
	}
}

// CreateParams groups the inputs for inserting a new custom emoji record.
type CreateParams struct {
	Name       string
	Animated   bool
	StorageKey string
	UploaderID uuid.UUID
}

// ValidateName checks that the emoji name meets naming requirements: 2 to 32 characters, alphanumeric and underscore
// only.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return ErrInvalidName
	}
	return nil
}

// Repository defines the data-access contract for custom emoji operations.
type Repository interface {
	// Create inserts a new custom emoji and returns the created record.
	Create(ctx context.Context, params CreateParams) (*Emoji, error)

	// GetByID returns a custom emoji by its ID. Returns ErrNotFound when the ID does not exist.
	GetByID(ctx context.Context, id uuid.UUID) (*Emoji, error)

	// GetByName returns a custom emoji by its unique name. Returns ErrNotFound when the name does not exist.
	GetByName(ctx context.Context, name string) (*Emoji, error)

	// List returns all custom emoji ordered by name.
	List(ctx context.Context) ([]Emoji, error)

	// UpdateName changes the name of an existing emoji. Returns ErrNotFound if the ID does not exist, or ErrNameTaken
	// if the new name conflicts with another emoji.
	UpdateName(ctx context.Context, id uuid.UUID, name string) (*Emoji, error)

	// Delete removes a custom emoji by ID. Returns ErrNotFound if the ID does not exist.
	Delete(ctx context.Context, id uuid.UUID) error

	// Count returns the total number of custom emoji.
	Count(ctx context.Context) (int, error)
}
