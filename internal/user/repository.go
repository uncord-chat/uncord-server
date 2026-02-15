package user

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/postgres"
)

// selectColumns lists the columns returned by queries that produce a *User. Every method that scans into a User must
// select these columns in this exact order.
const selectColumns = `id, email, username, display_name, avatar_key, pronouns, banner_key, about,
	theme_colour_primary, theme_colour_secondary, mfa_enabled, email_verified, created_at`

// selectCredentialsColumns lists the columns returned by queries that produce a *Credentials. The order must match
// scanCredentials.
const selectCredentialsColumns = `id, email, password_hash, username, display_name, avatar_key, pronouns, banner_key,
	about, theme_colour_primary, theme_colour_secondary, mfa_enabled, mfa_secret, email_verified`

// scanUser scans a single row into a *User. The row must contain the columns listed in selectColumns.
func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(
		&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.AvatarKey,
		&u.Pronouns, &u.BannerKey, &u.About, &u.ThemeColourPrimary, &u.ThemeColourSecondary,
		&u.MFAEnabled, &u.EmailVerified, &u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return &u, nil
}

// scanCredentials scans a single row into a *Credentials. The row must contain the columns listed in
// selectCredentialsColumns.
func scanCredentials(row pgx.Row) (*Credentials, error) {
	var c Credentials
	err := row.Scan(
		&c.ID, &c.Email, &c.PasswordHash, &c.Username, &c.DisplayName, &c.AvatarKey,
		&c.Pronouns, &c.BannerKey, &c.About, &c.ThemeColourPrimary, &c.ThemeColourSecondary,
		&c.MFAEnabled, &c.MFASecret, &c.EmailVerified,
	)
	if err != nil {
		return nil, fmt.Errorf("scan credentials: %w", err)
	}
	return &c, nil
}

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed user repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// Create inserts a new user and, when params.VerifyToken is non-empty, an email verification row, all inside a single
// transaction.
func (r *PGRepository) Create(ctx context.Context, params CreateParams) (uuid.UUID, error) {
	var userID uuid.UUID
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`INSERT INTO users (email, username, password_hash)
			 VALUES ($1, $2, $3)
			 RETURNING id`,
			params.Email, params.Username, params.PasswordHash,
		).Scan(&userID)
		if err != nil {
			if postgres.IsUniqueViolation(err) {
				return ErrAlreadyExists
			}
			return fmt.Errorf("insert user: %w", err)
		}

		if params.VerifyToken != "" {
			_, err = tx.Exec(ctx,
				`INSERT INTO email_verifications (user_id, token, expires_at)
				 VALUES ($1, $2, $3)`,
				userID, params.VerifyToken, params.VerifyExpiry,
			)
			if err != nil {
				return fmt.Errorf("insert email verification: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// GetByID returns the user matching the given ID.
func (r *PGRepository) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	u, err := scanUser(r.db.QueryRow(ctx, `SELECT `+selectColumns+` FROM users WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query user by id: %w", err)
	}
	return u, nil
}

// GetByEmail returns the user with credentials matching the given email address. This is one of two methods that return
// credentials, since it serves the authentication path.
func (r *PGRepository) GetByEmail(ctx context.Context, email string) (*Credentials, error) {
	c, err := scanCredentials(r.db.QueryRow(ctx,
		`SELECT `+selectCredentialsColumns+` FROM users WHERE email = $1`, email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query user by email: %w", err)
	}
	return c, nil
}

// GetCredentialsByID returns the user with credentials matching the given ID. Used by MFA flows that need the password
// hash and MFA secret after ticket-based user identification.
func (r *PGRepository) GetCredentialsByID(ctx context.Context, id uuid.UUID) (*Credentials, error) {
	c, err := scanCredentials(r.db.QueryRow(ctx,
		`SELECT `+selectCredentialsColumns+` FROM users WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query credentials by id: %w", err)
	}
	return c, nil
}

// VerifyEmail consumes a verification token and marks the user as verified, all within a single transaction.
func (r *PGRepository) VerifyEmail(ctx context.Context, token string) (uuid.UUID, error) {
	var userID uuid.UUID
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`UPDATE email_verifications
			 SET consumed_at = NOW()
			 WHERE token = $1 AND consumed_at IS NULL AND expires_at > NOW()
			 RETURNING user_id`,
			token,
		).Scan(&userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrInvalidToken
			}
			return fmt.Errorf("consume verification token: %w", err)
		}

		_, err = tx.Exec(ctx,
			`UPDATE users SET email_verified = true WHERE id = $1`,
			userID,
		)
		if err != nil {
			return fmt.Errorf("update email_verified: %w", err)
		}

		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// RecordLoginAttempt writes an entry to the login_attempts table.
func (r *PGRepository) RecordLoginAttempt(ctx context.Context, email, ipAddress string, success bool) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO login_attempts (email, ip_address, success) VALUES ($1, $2::inet, $3)`,
		email, ipAddress, success,
	)
	if err != nil {
		return fmt.Errorf("record login attempt: %w", err)
	}
	return nil
}

// UpdatePasswordHash updates the stored password hash for a user, used for lazy hash rotation when Argon2 parameters
// change.
func (r *PGRepository) UpdatePasswordHash(ctx context.Context, userID uuid.UUID, hash string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE users SET password_hash = $1 WHERE id = $2`,
		hash, userID,
	)
	if err != nil {
		return fmt.Errorf("update password hash: %w", err)
	}
	return nil
}

// Update applies the non-nil fields in params to the user row and returns the updated user. Returns ErrNotFound if no
// row matches the given ID.
func (r *PGRepository) Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*User, error) {
	var setClauses []string
	var args []any

	if params.DisplayName != nil {
		args = append(args, *params.DisplayName)
		setClauses = append(setClauses, "display_name = $"+strconv.Itoa(len(args)))
	}
	if params.AvatarKey != nil {
		args = append(args, *params.AvatarKey)
		setClauses = append(setClauses, "avatar_key = $"+strconv.Itoa(len(args)))
	}
	if params.Pronouns != nil {
		args = append(args, *params.Pronouns)
		setClauses = append(setClauses, "pronouns = $"+strconv.Itoa(len(args)))
	}
	if params.BannerKey != nil {
		args = append(args, *params.BannerKey)
		setClauses = append(setClauses, "banner_key = $"+strconv.Itoa(len(args)))
	}
	if params.About != nil {
		args = append(args, *params.About)
		setClauses = append(setClauses, "about = $"+strconv.Itoa(len(args)))
	}
	if params.ThemeColourPrimary != nil {
		args = append(args, *params.ThemeColourPrimary)
		setClauses = append(setClauses, "theme_colour_primary = $"+strconv.Itoa(len(args)))
	}
	if params.ThemeColourSecondary != nil {
		args = append(args, *params.ThemeColourSecondary)
		setClauses = append(setClauses, "theme_colour_secondary = $"+strconv.Itoa(len(args)))
	}

	// No fields to update. Return the current row without issuing an UPDATE so the database trigger does not bump
	// updated_at. A no-op PATCH should not alter the modification timestamp.
	if len(setClauses) == 0 {
		return r.GetByID(ctx, id)
	}

	args = append(args, id)
	query := "UPDATE users SET " + strings.Join(setClauses, ", ") +
		" WHERE id = $" + strconv.Itoa(len(args)) +
		" RETURNING " + selectColumns

	u, err := scanUser(r.db.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update user: %w", err)
	}
	return u, nil
}

// EnableMFA atomically sets the user's MFA secret and enabled flag, and inserts the initial set of recovery code
// hashes. All operations run in a single transaction.
func (r *PGRepository) EnableMFA(ctx context.Context, userID uuid.UUID, encryptedSecret string, codeHashes []string) error {
	return postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE users SET mfa_secret = $1, mfa_enabled = true WHERE id = $2`,
			encryptedSecret, userID,
		)
		if err != nil {
			return fmt.Errorf("update MFA columns: %w", err)
		}

		return copyRecoveryCodes(ctx, tx, userID, codeHashes)
	})
}

// DisableMFA atomically clears the user's MFA secret and enabled flag, and deletes all recovery codes.
func (r *PGRepository) DisableMFA(ctx context.Context, userID uuid.UUID) error {
	return postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE users SET mfa_secret = NULL, mfa_enabled = false WHERE id = $1`,
			userID,
		)
		if err != nil {
			return fmt.Errorf("clear MFA columns: %w", err)
		}

		_, err = tx.Exec(ctx,
			`DELETE FROM mfa_recovery_codes WHERE user_id = $1`,
			userID,
		)
		if err != nil {
			return fmt.Errorf("delete recovery codes: %w", err)
		}

		return nil
	})
}

// GetUnusedRecoveryCodes returns all recovery codes for the user that have not been consumed.
func (r *PGRepository) GetUnusedRecoveryCodes(ctx context.Context, userID uuid.UUID) ([]MFARecoveryCode, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, code_hash FROM mfa_recovery_codes WHERE user_id = $1 AND used_at IS NULL`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query unused recovery codes: %w", err)
	}
	defer rows.Close()

	var codes []MFARecoveryCode
	for rows.Next() {
		var c MFARecoveryCode
		if err := rows.Scan(&c.ID, &c.CodeHash); err != nil {
			return nil, fmt.Errorf("scan recovery code: %w", err)
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// UseRecoveryCode marks a recovery code as consumed by setting its used_at timestamp.
func (r *PGRepository) UseRecoveryCode(ctx context.Context, codeID uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE mfa_recovery_codes SET used_at = NOW() WHERE id = $1`,
		codeID,
	)
	if err != nil {
		return fmt.Errorf("mark recovery code used: %w", err)
	}
	return nil
}

// ReplaceRecoveryCodes deletes all existing recovery codes for the user and inserts new ones in a single transaction.
func (r *PGRepository) ReplaceRecoveryCodes(ctx context.Context, userID uuid.UUID, codeHashes []string) error {
	return postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id = $1`, userID)
		if err != nil {
			return fmt.Errorf("delete old recovery codes: %w", err)
		}

		return copyRecoveryCodes(ctx, tx, userID, codeHashes)
	})
}

// copyRecoveryCodes bulk-inserts recovery code hashes using CopyFrom, collapsing all rows into a single round trip.
func copyRecoveryCodes(ctx context.Context, tx pgx.Tx, userID uuid.UUID, codeHashes []string) error {
	rows := make([][]any, len(codeHashes))
	for i, hash := range codeHashes {
		rows[i] = []any{userID, hash}
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"mfa_recovery_codes"},
		[]string{"user_id", "code_hash"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copy recovery codes: %w", err)
	}
	return nil
}

// DeleteWithTombstones inserts deletion tombstones and deletes the user in a single transaction. Tombstone inserts use
// ON CONFLICT DO NOTHING so that re-deleting a restored account (or overlapping identifiers) is idempotent.
func (r *PGRepository) DeleteWithTombstones(ctx context.Context, id uuid.UUID, tombstones []Tombstone) error {
	return postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		for _, t := range tombstones {
			_, err := tx.Exec(ctx,
				`INSERT INTO deletion_tombstones (identifier_type, hmac_hash)
				 VALUES ($1, $2)
				 ON CONFLICT (identifier_type, hmac_hash) DO NOTHING`,
				string(t.IdentifierType), t.HMACHash,
			)
			if err != nil {
				return fmt.Errorf("insert tombstone: %w", err)
			}
		}

		tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}

		return nil
	})
}

// CheckTombstone returns true if a deletion tombstone exists for the given identifier type and HMAC hash.
func (r *PGRepository) CheckTombstone(ctx context.Context, identifierType TombstoneType, hmacHash string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM deletion_tombstones WHERE identifier_type = $1 AND hmac_hash = $2)`,
		string(identifierType), hmacHash,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check tombstone: %w", err)
	}
	return exists, nil
}

// purgeBatchSize is the maximum number of rows deleted per batch to avoid long-running transactions.
const purgeBatchSize = 1000

// PurgeLoginAttempts deletes login attempt rows older than the given cutoff in batches.
func (r *PGRepository) PurgeLoginAttempts(ctx context.Context, olderThan time.Time) (int64, error) {
	if r.db == nil {
		return 0, fmt.Errorf("purge login attempts: database pool is nil")
	}

	const query = `DELETE FROM login_attempts WHERE ctid IN (SELECT ctid FROM login_attempts WHERE created_at < $1 LIMIT 1000)`

	var total int64
	for {
		tag, err := r.db.Exec(ctx, query, olderThan)
		if err != nil {
			return total, fmt.Errorf("purge login attempts: %w", err)
		}
		affected := tag.RowsAffected()
		total += affected
		if affected < purgeBatchSize {
			break
		}
	}
	return total, nil
}

// PurgeTombstones deletes deletion tombstone rows older than the given cutoff in batches.
func (r *PGRepository) PurgeTombstones(ctx context.Context, olderThan time.Time) (int64, error) {
	if r.db == nil {
		return 0, fmt.Errorf("purge deletion tombstones: database pool is nil")
	}

	const query = `DELETE FROM deletion_tombstones WHERE ctid IN (SELECT ctid FROM deletion_tombstones WHERE created_at < $1 LIMIT 1000)`

	var total int64
	for {
		tag, err := r.db.Exec(ctx, query, olderThan)
		if err != nil {
			return total, fmt.Errorf("purge deletion tombstones: %w", err)
		}
		affected := tag.RowsAffected()
		total += affected
		if affected < purgeBatchSize {
			break
		}
	}
	return total, nil
}
