package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for the server config repository.
var (
	ErrNotFound          = errors.New("server config not found")
	ErrNameLength        = errors.New("name must be between 1 and 100 characters")
	ErrDescriptionLength = errors.New("description must be 1024 characters or fewer")
)

// Config holds the server configuration read from the database.
type Config struct {
	ID          uuid.UUID
	Name        string
	Description string
	IconKey     *string
	BannerKey   *string
	OwnerID     uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UpdateParams groups the optional fields for updating the server configuration.
type UpdateParams struct {
	Name        *string
	Description *string
	IconKey     *string
	BannerKey   *string
}

// ValidateName checks that a non-nil name is between 1 and 100 characters after trimming whitespace. On success the
// pointed-to value is replaced with the trimmed result.
func ValidateName(name *string) error {
	if name == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*name)
	if len(trimmed) < 1 || len(trimmed) > 100 {
		return ErrNameLength
	}
	*name = trimmed
	return nil
}

// ValidateDescription checks that a non-nil description is 1024 characters or fewer.
func ValidateDescription(desc *string) error {
	if desc == nil {
		return nil
	}
	if len(*desc) > 1024 {
		return ErrDescriptionLength
	}
	return nil
}

// Repository defines the data-access contract for server config operations.
type Repository interface {
	Get(ctx context.Context) (*Config, error)
	Update(ctx context.Context, params UpdateParams) (*Config, error)
}
