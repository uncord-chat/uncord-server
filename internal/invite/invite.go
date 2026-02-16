package invite

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for the invite package.
var (
	ErrNotFound        = errors.New("invite not found")
	ErrExpired         = errors.New("invite has expired")
	ErrMaxUsesReached  = errors.New("invite has reached its maximum number of uses")
	ErrChannelNotFound = errors.New("channel not found")
	ErrCodeLength      = errors.New("failed to generate unique invite code")
	ErrInvalidMaxUses  = errors.New("max uses must be non-negative")
	ErrInvalidMaxAge   = errors.New("max age seconds must be non-negative")
)

// Pagination defaults.
const (
	DefaultLimit = 50
	MaxLimit     = 100
)

// Invite holds the fields read from the invites table.
type Invite struct {
	ID            uuid.UUID
	Code          string
	ChannelID     uuid.UUID
	CreatorID     uuid.UUID
	MaxUses       *int
	UseCount      int
	MaxAgeSeconds *int
	ExpiresAt     *time.Time
	CreatedAt     time.Time
}

// OnboardingConfig holds the onboarding requirements read from the onboarding_config table.
type OnboardingConfig struct {
	WelcomeChannelID         *uuid.UUID
	RequireRulesAcceptance   bool
	RequireEmailVerification bool
	MinAccountAgeSeconds     int
	RequirePhone             bool
	RequireCaptcha           bool
	AutoRoles                []uuid.UUID
}

// CreateParams groups the inputs for creating a new invite.
type CreateParams struct {
	ChannelID     uuid.UUID
	MaxUses       *int
	MaxAgeSeconds *int
}

// ValidateMaxUses checks that a non-nil max uses value is non-negative.
func ValidateMaxUses(v *int) error {
	if v != nil && *v < 0 {
		return ErrInvalidMaxUses
	}
	return nil
}

// ValidateMaxAge checks that a non-nil max age seconds value is non-negative.
func ValidateMaxAge(v *int) error {
	if v != nil && *v < 0 {
		return ErrInvalidMaxAge
	}
	return nil
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

// Repository defines the data-access contract for invite operations.
type Repository interface {
	Create(ctx context.Context, creatorID uuid.UUID, params CreateParams) (*Invite, error)
	GetByCode(ctx context.Context, code string) (*Invite, error)
	List(ctx context.Context, after *uuid.UUID, limit int) ([]Invite, error)
	Delete(ctx context.Context, code string) error
	Use(ctx context.Context, code string) (*Invite, error)
	GetOnboardingConfig(ctx context.Context) (*OnboardingConfig, error)
}
