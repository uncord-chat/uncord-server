package role

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/uncord-chat/uncord-protocol/models"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

// Sentinel errors for the role package.
var (
	ErrNotFound           = errors.New("role not found")
	ErrAlreadyExists      = errors.New("role name or position already taken")
	ErrNameLength         = errors.New("role name must be between 1 and 100 characters")
	ErrInvalidPosition    = errors.New("position must be non-negative")
	ErrInvalidPermissions = errors.New("permissions bitfield contains invalid bits")
	ErrInvalidColour      = errors.New("colour must be between 0 and 16777215")
	ErrMaxRolesReached    = errors.New("maximum number of roles reached")
	ErrEveryoneImmutable  = errors.New("the @everyone role cannot be deleted")
)

// Role holds the fields read from the database.
type Role struct {
	ID          uuid.UUID
	Name        string
	Colour      int
	Position    int
	Hoist       bool
	Permissions int64
	IsEveryone  bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ToModel converts the internal role struct to the protocol response type.
func (r *Role) ToModel() models.Role {
	return models.Role{
		ID:          r.ID.String(),
		Name:        r.Name,
		Colour:      r.Colour,
		Position:    r.Position,
		Hoist:       r.Hoist,
		Permissions: r.Permissions,
		IsEveryone:  r.IsEveryone,
		CreatedAt:   r.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   r.UpdatedAt.Format(time.RFC3339),
	}
}

// CreateParams groups the inputs for creating a new role.
type CreateParams struct {
	Name        string
	Colour      int
	Permissions int64
	Hoist       bool
}

// UpdateParams groups the optional fields for updating a role.
type UpdateParams struct {
	Name        *string
	Colour      *int
	Position    *int
	Permissions *int64
	Hoist       *bool
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

// ValidatePermissions checks that a non-nil permissions bitfield contains only valid permission bits.
func ValidatePermissions(perms *int64) error {
	if perms == nil {
		return nil
	}
	all := int64(permissions.AllPermissions)
	if *perms < 0 || *perms & ^all != 0 {
		return ErrInvalidPermissions
	}
	return nil
}

// ValidateColour checks that a non-nil colour is in the valid RGB range (0 to 0xFFFFFF).
func ValidateColour(colour *int) error {
	if colour == nil {
		return nil
	}
	if *colour < 0 || *colour > 0xFFFFFF {
		return ErrInvalidColour
	}
	return nil
}

// Repository defines the data-access contract for role operations.
type Repository interface {
	List(ctx context.Context) ([]Role, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Role, error)
	Create(ctx context.Context, params CreateParams, maxRoles int) (*Role, error)
	Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*Role, error)
	Delete(ctx context.Context, id uuid.UUID) error
	HighestPosition(ctx context.Context, userID uuid.UUID) (int, error)
}
