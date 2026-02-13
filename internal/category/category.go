package category

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Sentinel errors for the category repository.
var (
	ErrNotFound             = errors.New("category not found")
	ErrAlreadyExists        = errors.New("category position already taken")
	ErrMaxCategoriesReached = errors.New("maximum number of categories reached")
	ErrNameLength           = errors.New("category name must be between 1 and 100 characters")
	ErrInvalidPosition      = errors.New("position must be non-negative")
)

// Category holds the fields read from the database.
type Category struct {
	ID        uuid.UUID
	Name      string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CreateParams groups the inputs for creating a new category.
type CreateParams struct {
	Name string
}

// UpdateParams groups the optional fields for updating a category.
type UpdateParams struct {
	Name     *string
	Position *int
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

// ValidateNameRequired validates and trims a name that must be present. It returns the trimmed result on success.
func ValidateNameRequired(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 100 {
		return "", ErrNameLength
	}
	return trimmed, nil
}

// ValidatePosition checks that a non-nil position is non-negative. A nil pointer means "no change."
func ValidatePosition(pos *int) error {
	if pos == nil {
		return nil
	}
	if *pos < 0 {
		return ErrInvalidPosition
	}
	return nil
}

// Repository defines the data-access contract for category operations.
type Repository interface {
	List(ctx context.Context) ([]Category, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Category, error)
	Create(ctx context.Context, params CreateParams, maxCategories int) (*Category, error)
	Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*Category, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
