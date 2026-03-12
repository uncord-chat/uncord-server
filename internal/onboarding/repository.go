package onboarding

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const selectColumns = `id, welcome_channel_id, require_email_verification, open_join, min_account_age_seconds,
	require_phone, require_captcha, auto_roles, created_at, updated_at`

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed onboarding config repository.
func NewPGRepository(db *pgxpool.Pool) *PGRepository {
	return &PGRepository{db: db}
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

// Update applies the non-nil fields in params to the onboarding config row and returns the updated config. Nil pointer
// fields are left unchanged via COALESCE; the nullable welcome_channel_id column and the auto_roles array use CASE
// expressions so that SetWelcomeChannelNull and SetAutoRoles can explicitly clear or replace them. All values flow
// through pgx named parameter binding.
func (r *PGRepository) Update(ctx context.Context, params UpdateParams) (*Config, error) {
	// No fields to update. Return the current row without issuing an UPDATE so the database trigger does not bump
	// updated_at. A no-op PATCH should not alter the modification timestamp.
	if !params.SetWelcomeChannelNull && params.WelcomeChannelID == nil &&
		params.RequireEmailVerification == nil && params.OpenJoin == nil &&
		params.MinAccountAgeSeconds == nil && !params.SetAutoRoles {
		return r.Get(ctx)
	}

	const query = `UPDATE onboarding_config SET
		welcome_channel_id         = CASE WHEN @clear_welcome_channel THEN NULL
		                                  ELSE COALESCE(@welcome_channel_id, welcome_channel_id) END,
		require_email_verification = COALESCE(@require_email_verification, require_email_verification),
		open_join                  = COALESCE(@open_join, open_join),
		min_account_age_seconds    = COALESCE(@min_account_age_seconds, min_account_age_seconds),
		auto_roles                 = CASE WHEN @set_auto_roles THEN @auto_roles ELSE auto_roles END
		RETURNING ` + selectColumns

	args := pgx.NamedArgs{
		"clear_welcome_channel":      params.SetWelcomeChannelNull,
		"welcome_channel_id":         params.WelcomeChannelID,
		"require_email_verification": params.RequireEmailVerification,
		"open_join":                  params.OpenJoin,
		"min_account_age_seconds":    params.MinAccountAgeSeconds,
		"set_auto_roles":             params.SetAutoRoles,
		"auto_roles":                 params.AutoRoles,
	}

	row := r.db.QueryRow(ctx, query, args)
	cfg, err := scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update onboarding config: %w", err)
	}
	return cfg, nil
}

// RecordAcceptances inserts document acceptance records for a user. Existing acceptances for the same slug are left
// unchanged (ON CONFLICT DO NOTHING), preserving the original accepted_at timestamp.
func (r *PGRepository) RecordAcceptances(ctx context.Context, userID uuid.UUID, slugs []string) error {
	if len(slugs) == 0 {
		return nil
	}

	const query = `INSERT INTO document_acceptances (user_id, slug) VALUES (@user_id, @slug) ON CONFLICT DO NOTHING`

	batch := &pgx.Batch{}
	for _, slug := range slugs {
		batch.Queue(query, pgx.NamedArgs{"user_id": userID, "slug": slug})
	}

	results := r.db.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()

	for range slugs {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("record document acceptance: %w", err)
		}
	}
	return nil
}

// GetAcceptances returns all document acceptance records for a user, ordered by acceptance time.
func (r *PGRepository) GetAcceptances(ctx context.Context, userID uuid.UUID) ([]Acceptance, error) {
	const query = `SELECT slug, accepted_at FROM document_acceptances WHERE user_id = @user_id ORDER BY accepted_at`

	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"user_id": userID})
	if err != nil {
		return nil, fmt.Errorf("query document acceptances: %w", err)
	}
	defer rows.Close()

	var acceptances []Acceptance
	for rows.Next() {
		var a Acceptance
		if err := rows.Scan(&a.Slug, &a.AcceptedAt); err != nil {
			return nil, fmt.Errorf("scan document acceptance: %w", err)
		}
		acceptances = append(acceptances, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate document acceptances: %w", err)
	}
	return acceptances, nil
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
		return nil, fmt.Errorf("scan onboarding config: %w", err)
	}
	return &cfg, nil
}
