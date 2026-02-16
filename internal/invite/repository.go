package invite

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/postgres"
)

const (
	codeLength     = 8
	codeAlphabet   = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	maxCodeRetries = 3
)

// selectColumns lists the columns returned by queries that produce an *Invite.
const selectColumns = `id, code, channel_id, creator_id, max_uses, use_count, max_age_seconds, expires_at, created_at`

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed invite repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// Create inserts a new invite with a randomly generated code. If the channel does not exist, ErrChannelNotFound is
// returned. Code generation retries up to maxCodeRetries on the unlikely event of a unique constraint violation.
func (r *PGRepository) Create(ctx context.Context, creatorID uuid.UUID, params CreateParams) (*Invite, error) {
	var expiresAt *time.Time
	if params.MaxAgeSeconds != nil && *params.MaxAgeSeconds > 0 {
		t := time.Now().Add(time.Duration(*params.MaxAgeSeconds) * time.Second)
		expiresAt = &t
	}

	for attempt := range maxCodeRetries {
		code, err := generateCode()
		if err != nil {
			return nil, fmt.Errorf("generate invite code: %w", err)
		}

		inv, err := scanInvite(r.db.QueryRow(ctx,
			`INSERT INTO invites (code, channel_id, creator_id, max_uses, max_age_seconds, expires_at)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 RETURNING `+selectColumns,
			code, params.ChannelID, creatorID, params.MaxUses, params.MaxAgeSeconds, expiresAt,
		))
		if err != nil {
			if postgres.IsForeignKeyViolation(err) {
				return nil, ErrChannelNotFound
			}
			if postgres.IsUniqueViolation(err) && attempt < maxCodeRetries-1 {
				continue
			}
			if postgres.IsUniqueViolation(err) {
				return nil, ErrCodeLength
			}
			return nil, fmt.Errorf("insert invite: %w", err)
		}
		return inv, nil
	}

	return nil, ErrCodeLength
}

// GetByCode returns the invite matching the given code.
func (r *PGRepository) GetByCode(ctx context.Context, code string) (*Invite, error) {
	inv, err := scanInvite(r.db.QueryRow(ctx,
		`SELECT `+selectColumns+` FROM invites WHERE code = $1`, code))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query invite by code: %w", err)
	}
	return inv, nil
}

// List returns invites ordered by (created_at DESC, id) using keyset pagination.
func (r *PGRepository) List(ctx context.Context, after *uuid.UUID, limit int) ([]Invite, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if after == nil {
		rows, err = r.db.Query(ctx,
			`SELECT `+selectColumns+` FROM invites
			 ORDER BY created_at DESC, id
			 LIMIT $1`, limit)
	} else {
		rows, err = r.db.Query(ctx,
			`SELECT `+selectColumns+` FROM invites
			 WHERE (created_at, id) < (SELECT created_at, id FROM invites WHERE id = $1)
			 ORDER BY created_at DESC, id
			 LIMIT $2`, *after, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("query invites: %w", err)
	}
	defer rows.Close()

	var invites []Invite
	for rows.Next() {
		var inv Invite
		if err := rows.Scan(
			&inv.ID, &inv.Code, &inv.ChannelID, &inv.CreatorID,
			&inv.MaxUses, &inv.UseCount, &inv.MaxAgeSeconds, &inv.ExpiresAt, &inv.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan invite: %w", err)
		}
		invites = append(invites, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invites: %w", err)
	}
	return invites, nil
}

// Delete removes an invite by code. Returns ErrNotFound if no matching invite exists.
func (r *PGRepository) Delete(ctx context.Context, code string) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM invites WHERE code = $1", code)
	if err != nil {
		return fmt.Errorf("delete invite: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Use atomically increments the use count of a valid invite and returns the updated invite. The invite must not be
// expired and must not have reached its maximum uses. If the atomic update affects zero rows, a diagnostic query
// determines the specific reason for failure.
func (r *PGRepository) Use(ctx context.Context, code string) (*Invite, error) {
	inv, err := scanInvite(r.db.QueryRow(ctx,
		`UPDATE invites
		 SET use_count = use_count + 1
		 WHERE code = $1
		   AND (expires_at IS NULL OR expires_at > NOW())
		   AND (max_uses IS NULL OR use_count < max_uses)
		 RETURNING `+selectColumns,
		code,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, r.diagnoseUseFailure(ctx, code)
		}
		return nil, fmt.Errorf("use invite: %w", err)
	}
	return inv, nil
}

// diagnoseUseFailure determines why an atomic use update matched zero rows.
func (r *PGRepository) diagnoseUseFailure(ctx context.Context, code string) error {
	var (
		expiresAt *time.Time
		maxUses   *int
		useCount  int
	)
	err := r.db.QueryRow(ctx,
		"SELECT expires_at, max_uses, use_count FROM invites WHERE code = $1", code,
	).Scan(&expiresAt, &maxUses, &useCount)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("diagnose invite use failure: %w", err)
	}

	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return ErrExpired
	}
	if maxUses != nil && useCount >= *maxUses {
		return ErrMaxUsesReached
	}
	return ErrNotFound
}

// GetOnboardingConfig reads the single onboarding_config row. Returns a zero-value config if no row exists.
func (r *PGRepository) GetOnboardingConfig(ctx context.Context) (*OnboardingConfig, error) {
	var cfg OnboardingConfig
	err := r.db.QueryRow(ctx,
		`SELECT welcome_channel_id, require_rules_acceptance, require_email_verification,
		        min_account_age_seconds, require_phone, require_captcha, auto_roles
		 FROM onboarding_config
		 LIMIT 1`,
	).Scan(
		&cfg.WelcomeChannelID, &cfg.RequireRulesAcceptance, &cfg.RequireEmailVerification,
		&cfg.MinAccountAgeSeconds, &cfg.RequirePhone, &cfg.RequireCaptcha, &cfg.AutoRoles,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &OnboardingConfig{}, nil
		}
		return nil, fmt.Errorf("query onboarding config: %w", err)
	}
	return &cfg, nil
}

// scanInvite scans a single row into an *Invite.
func scanInvite(row pgx.Row) (*Invite, error) {
	var inv Invite
	err := row.Scan(
		&inv.ID, &inv.Code, &inv.ChannelID, &inv.CreatorID,
		&inv.MaxUses, &inv.UseCount, &inv.MaxAgeSeconds, &inv.ExpiresAt, &inv.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan invite: %w", err)
	}
	return &inv, nil
}

// generateCode produces a cryptographically random alphanumeric string of codeLength characters.
func generateCode() (string, error) {
	alphabetLen := big.NewInt(int64(len(codeAlphabet)))
	buf := make([]byte, codeLength)
	for i := range buf {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", fmt.Errorf("crypto/rand: %w", err)
		}
		buf[i] = codeAlphabet[n.Int64()]
	}
	return string(buf), nil
}
