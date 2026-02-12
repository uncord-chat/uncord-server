package user

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for the user repository.
var (
	ErrNotFound      = errors.New("user not found")
	ErrAlreadyExists = errors.New("email or username already taken")
	ErrInvalidToken  = errors.New("invalid or expired verification token")
)

// User holds the core identity fields read from the database.
type User struct {
	ID            uuid.UUID
	Email         string
	Username      string
	PasswordHash  string
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

// Repository defines the data-access contract for user operations.
type Repository interface {
	Create(ctx context.Context, params CreateParams) (uuid.UUID, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	VerifyEmail(ctx context.Context, token string) (uuid.UUID, error)
	RecordLoginAttempt(ctx context.Context, email, ipAddress string, success bool) error
}
