package onboarding

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/uncord-chat/uncord-protocol/models"
)

// Sentinel errors for the onboarding package.
var (
	ErrNotFound            = errors.New("onboarding config not found")
	ErrOpenJoinDisabled    = errors.New("open server joining is not enabled")
	ErrDocumentsIncomplete = errors.New("not all required documents have been accepted")
)

// Config holds the onboarding configuration read from the database.
type Config struct {
	ID                       uuid.UUID
	WelcomeChannelID         *uuid.UUID
	RequireEmailVerification bool
	OpenJoin                 bool
	MinAccountAgeSeconds     int
	RequirePhone             bool
	RequireCaptcha           bool
	AutoRoles                []uuid.UUID
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// ToModel converts the internal config to the protocol response type. The caller provides the document list because
// documents are loaded from the filesystem, not the database.
func (cfg *Config) ToModel(docs []models.OnboardingDocument) models.OnboardingConfig {
	var welcomeChannelID *string
	if cfg.WelcomeChannelID != nil {
		s := cfg.WelcomeChannelID.String()
		welcomeChannelID = &s
	}

	autoRoles := make([]string, len(cfg.AutoRoles))
	for i, id := range cfg.AutoRoles {
		autoRoles[i] = id.String()
	}

	return models.OnboardingConfig{
		WelcomeChannelID:         welcomeChannelID,
		RequireEmailVerification: cfg.RequireEmailVerification,
		OpenJoin:                 cfg.OpenJoin,
		MinAccountAgeSeconds:     cfg.MinAccountAgeSeconds,
		AutoRoles:                autoRoles,
		Documents:                docs,
	}
}

// UpdateParams groups the optional fields for updating the onboarding configuration. Nil pointer fields indicate "no
// change" (PATCH semantics).
type UpdateParams struct {
	WelcomeChannelID         *uuid.UUID
	SetWelcomeChannelNull    bool
	RequireEmailVerification *bool
	OpenJoin                 *bool
	MinAccountAgeSeconds     *int
	AutoRoles                []uuid.UUID
	SetAutoRoles             bool
}

// Repository defines the data access contract for onboarding config operations.
type Repository interface {
	Get(ctx context.Context) (*Config, error)
	Update(ctx context.Context, params UpdateParams) (*Config, error)
}
