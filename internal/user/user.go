package user

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Sentinel errors for the user repository.
var (
	ErrNotFound          = errors.New("user not found")
	ErrAlreadyExists     = errors.New("email or username already taken")
	ErrInvalidToken      = errors.New("invalid or expired verification token")
	ErrDisplayNameLength = errors.New("display name must be between 1 and 32 characters")
)

// User holds the core identity fields read from the database.
type User struct {
	ID            uuid.UUID
	Email         string
	Username      string
	PasswordHash  string
	DisplayName   *string
	AvatarKey     *string
	MFAEnabled    bool
	EmailVerified bool
}

// CreateParams groups the inputs for creating a new user. When VerifyToken is non-empty, an email_verifications row is
// inserted in the same transaction.
type CreateParams struct {
	Email        string
	Username     string
	PasswordHash string
	VerifyToken  string
	VerifyExpiry time.Time
}

// UpdateParams groups the optional fields for updating a user profile.
type UpdateParams struct {
	DisplayName *string
	AvatarKey   *string
}

// ValidateDisplayName checks that a non-nil display name is between 1 and 32 characters after trimming whitespace.
func ValidateDisplayName(name *string) error {
	if name == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*name)
	if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 32 {
		return ErrDisplayNameLength
	}
	return nil
}

// Repository defines the data-access contract for user operations.
type Repository interface {
	Create(ctx context.Context, params CreateParams) (uuid.UUID, error)
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	VerifyEmail(ctx context.Context, token string) (uuid.UUID, error)
	RecordLoginAttempt(ctx context.Context, email, ipAddress string, success bool) error
	UpdatePasswordHash(ctx context.Context, userID uuid.UUID, hash string) error
	Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*User, error)
}
