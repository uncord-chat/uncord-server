package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"github.com/uncord-chat/uncord-protocol/permissions"

	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/config"
)

var sanitizeUsername = regexp.MustCompile(`[^a-zA-Z0-9_.]`)

// DefaultEveryonePermissions is the permission bitfield assigned to the @everyone role during first-run initialization.
var DefaultEveryonePermissions = permissions.ViewChannels |
	permissions.SendMessages |
	permissions.ReadMessageHistory |
	permissions.AddReactions |
	permissions.CreateInvites |
	permissions.ChangeNicknames |
	permissions.VoiceConnect |
	permissions.VoiceSpeak |
	permissions.VoicePTT

// IsFirstRun returns true when the server_config table has no rows.
func IsFirstRun(ctx context.Context, db *pgxpool.Pool) (bool, error) {
	var count int
	err := db.QueryRow(ctx, "SELECT COUNT(*) FROM server_config").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check first run: %w", err)
	}
	return count == 0, nil
}

// RunFirstInit seeds the database with the owner account, default roles, channels, and onboarding config inside a
// single transaction.
func RunFirstInit(ctx context.Context, db *pgxpool.Pool, cfg *config.Config) error {
	if cfg.InitOwnerEmail == "" || cfg.InitOwnerPassword == "" {
		return fmt.Errorf("INIT_OWNER_EMAIL and INIT_OWNER_PASSWORD must be set for first-run initialization")
	}

	ownerEmail, _, err := auth.ValidateEmail(cfg.InitOwnerEmail)
	if err != nil {
		return fmt.Errorf("invalid INIT_OWNER_EMAIL: %w", err)
	}

	hash, err := auth.HashPassword(
		cfg.InitOwnerPassword,
		cfg.Argon2Memory,
		cfg.Argon2Iterations,
		cfg.Argon2Parallelism,
		cfg.Argon2SaltLength,
		cfg.Argon2KeyLength,
	)
	if err != nil {
		return fmt.Errorf("hash owner password: %w", err)
	}

	// Derive username from email local part, stripping invalid characters.
	username := ownerEmail
	if idx := strings.Index(username, "@"); idx > 0 {
		username = username[:idx]
	}
	username = sanitizeUsername.ReplaceAllString(username, "")
	if err := auth.ValidateUsername(username); err != nil {
		return fmt.Errorf("derived owner username %q from email is invalid: %w", username, err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin init transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Warn().Err(err).Msg("tx rollback failed")
		}
	}()

	// Insert owner user
	var ownerID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, username, password_hash, email_verified)
		 VALUES ($1, $2, $3, true)
		 RETURNING id`,
		ownerEmail, username, hash,
	).Scan(&ownerID)
	if err != nil {
		return fmt.Errorf("insert owner user: %w", err)
	}

	// Insert server_config
	_, err = tx.Exec(ctx,
		`INSERT INTO server_config (name, description, owner_id)
		 VALUES ($1, $2, $3)`,
		cfg.ServerName, cfg.ServerDescription, ownerID,
	)
	if err != nil {
		return fmt.Errorf("insert server_config: %w", err)
	}

	// Insert @everyone role
	var everyoneRoleID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO roles (name, position, is_everyone, permissions)
		 VALUES ('@everyone', 0, true, $1)
		 RETURNING id`,
		int64(DefaultEveryonePermissions),
	).Scan(&everyoneRoleID)
	if err != nil {
		return fmt.Errorf("insert @everyone role: %w", err)
	}

	// Insert owner as member
	_, err = tx.Exec(ctx,
		`INSERT INTO members (user_id, status) VALUES ($1, 'active')`,
		ownerID,
	)
	if err != nil {
		return fmt.Errorf("insert owner member: %w", err)
	}

	// Assign @everyone role to owner
	_, err = tx.Exec(ctx,
		`INSERT INTO member_roles (user_id, role_id) VALUES ($1, $2)`,
		ownerID, everyoneRoleID,
	)
	if err != nil {
		return fmt.Errorf("insert owner member_roles: %w", err)
	}

	// Insert #general channel
	_, err = tx.Exec(ctx,
		`INSERT INTO channels (name, type, position) VALUES ('general', 'text', 0)`,
	)
	if err != nil {
		return fmt.Errorf("insert #general channel: %w", err)
	}

	// Insert #welcome channel
	var welcomeChannelID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO channels (name, type, position) VALUES ('welcome', 'text', 1) RETURNING id`,
	).Scan(&welcomeChannelID)
	if err != nil {
		return fmt.Errorf("insert #welcome channel: %w", err)
	}

	// Insert onboarding_config
	_, err = tx.Exec(ctx,
		`INSERT INTO onboarding_config (
			welcome_channel_id,
			require_rules_acceptance,
			require_email_verification,
			min_account_age_seconds,
			require_phone,
			require_captcha
		) VALUES ($1, $2, $3, $4, $5, $6)`,
		welcomeChannelID,
		cfg.OnboardingRequireRules,
		cfg.OnboardingRequireEmailVerification,
		cfg.OnboardingMinAccountAge,
		cfg.OnboardingRequirePhone,
		cfg.OnboardingRequireCaptcha,
	)
	if err != nil {
		return fmt.Errorf("insert onboarding_config: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit init transaction: %w", err)
	}

	return nil
}
