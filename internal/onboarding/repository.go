package onboarding

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const selectColumns = `id, welcome_channel_id, require_email_verification, open_join, min_account_age_seconds,
	require_phone, require_captcha, auto_roles, created_at, updated_at`

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed onboarding config repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// Get returns the single onboarding config row.
func (r *PGRepository) Get(ctx context.Context) (*Config, error) {
	row := r.db.QueryRow(ctx, "SELECT "+selectColumns+" FROM onboarding_config LIMIT 1")
	cfg, err := scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query onboarding config: %w", err)
	}
	return cfg, nil
}

// Update applies the non-nil fields in params to the onboarding config row and returns the updated config.
func (r *PGRepository) Update(ctx context.Context, params UpdateParams) (*Config, error) {
	var setClauses []string
	namedArgs := pgx.NamedArgs{}

	if params.SetWelcomeChannelNull {
		setClauses = append(setClauses, "welcome_channel_id = NULL")
	} else if params.WelcomeChannelID != nil {
		setClauses = append(setClauses, "welcome_channel_id = @welcome_channel_id")
		namedArgs["welcome_channel_id"] = *params.WelcomeChannelID
	}

	if params.RequireEmailVerification != nil {
		setClauses = append(setClauses, "require_email_verification = @require_email_verification")
		namedArgs["require_email_verification"] = *params.RequireEmailVerification
	}

	if params.OpenJoin != nil {
		setClauses = append(setClauses, "open_join = @open_join")
		namedArgs["open_join"] = *params.OpenJoin
	}

	if params.MinAccountAgeSeconds != nil {
		setClauses = append(setClauses, "min_account_age_seconds = @min_account_age_seconds")
		namedArgs["min_account_age_seconds"] = *params.MinAccountAgeSeconds
	}

	if params.SetAutoRoles {
		setClauses = append(setClauses, "auto_roles = @auto_roles")
		namedArgs["auto_roles"] = params.AutoRoles
	}

	if len(setClauses) == 0 {
		return r.Get(ctx)
	}

	query := "UPDATE onboarding_config SET " + strings.Join(setClauses, ", ") +
		" RETURNING " + selectColumns

	row := r.db.QueryRow(ctx, query, namedArgs)
	cfg, err := scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update onboarding config: %w", err)
	}
	return cfg, nil
}

// scanConfig scans a single row into a Config struct.
func scanConfig(row pgx.Row) (*Config, error) {
	var cfg Config
	err := row.Scan(
		&cfg.ID, &cfg.WelcomeChannelID, &cfg.RequireEmailVerification, &cfg.OpenJoin,
		&cfg.MinAccountAgeSeconds, &cfg.RequirePhone, &cfg.RequireCaptcha, &cfg.AutoRoles,
		&cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
