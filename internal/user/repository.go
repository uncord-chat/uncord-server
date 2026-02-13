package user

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

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
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin create user tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			r.log.Warn().Err(err).Msg("tx rollback failed")
		}
	}()

	var userID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, username, password_hash)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		params.Email, params.Username, params.PasswordHash,
	).Scan(&userID)
	if err != nil {
		if isUniqueViolation(err) {
			return uuid.Nil, ErrAlreadyExists
		}
		return uuid.Nil, fmt.Errorf("insert user: %w", err)
	}

	if params.VerifyToken != "" {
		_, err = tx.Exec(ctx,
			`INSERT INTO email_verifications (user_id, token, expires_at)
			 VALUES ($1, $2, $3)`,
			userID, params.VerifyToken, params.VerifyExpiry,
		)
		if err != nil {
			return uuid.Nil, fmt.Errorf("insert email verification: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit create user tx: %w", err)
	}

	return userID, nil
}

// GetByID returns the user matching the given ID.
func (r *PGRepository) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User
	err := r.db.QueryRow(ctx,
		`SELECT id, email, username, display_name, avatar_key, mfa_enabled, email_verified
		 FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.AvatarKey, &u.MFAEnabled, &u.EmailVerified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query user by id: %w", err)
	}
	return &u, nil
}

// GetByEmail returns the user with credentials matching the given email address. This is the only read method that
// returns credentials, since it serves the authentication path.
func (r *PGRepository) GetByEmail(ctx context.Context, email string) (*Credentials, error) {
	var c Credentials
	err := r.db.QueryRow(ctx,
		`SELECT id, email, password_hash, username, display_name, avatar_key, mfa_enabled, email_verified
		 FROM users WHERE email = $1`,
		email,
	).Scan(&c.ID, &c.Email, &c.PasswordHash, &c.Username, &c.DisplayName, &c.AvatarKey, &c.MFAEnabled, &c.EmailVerified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query user by email: %w", err)
	}
	return &c, nil
}

// VerifyEmail consumes a verification token and marks the user as verified, all within a single transaction.
func (r *PGRepository) VerifyEmail(ctx context.Context, token string) (uuid.UUID, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin verify email tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			r.log.Warn().Err(err).Msg("tx rollback failed")
		}
	}()

	var userID uuid.UUID
	err = tx.QueryRow(ctx,
		`UPDATE email_verifications
		 SET consumed_at = NOW()
		 WHERE token = $1 AND consumed_at IS NULL AND expires_at > NOW()
		 RETURNING user_id`,
		token,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrInvalidToken
		}
		return uuid.Nil, fmt.Errorf("consume verification token: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE users SET email_verified = true WHERE id = $1`,
		userID,
	)
	if err != nil {
		return uuid.Nil, fmt.Errorf("update email_verified: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit verify email tx: %w", err)
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
	setClauses := make([]string, 0, 2)
	args := make([]any, 0, 3)
	argPos := 1

	if params.DisplayName != nil {
		setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argPos))
		args = append(args, *params.DisplayName)
		argPos++
	}
	if params.AvatarKey != nil {
		setClauses = append(setClauses, fmt.Sprintf("avatar_key = $%d", argPos))
		args = append(args, *params.AvatarKey)
		argPos++
	}

	// No fields to update. Return the current row without issuing an UPDATE so the database trigger does not bump
	// updated_at. A no-op PATCH should not alter the modification timestamp.
	if len(setClauses) == 0 {
		return r.GetByID(ctx, id)
	}

	query := fmt.Sprintf(
		`UPDATE users SET %s WHERE id = $%d
		 RETURNING id, email, username, display_name, avatar_key, mfa_enabled, email_verified`,
		strings.Join(setClauses, ", "), argPos,
	)
	args = append(args, id)

	var u User
	err := r.db.QueryRow(ctx, query, args...).
		Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.AvatarKey, &u.MFAEnabled, &u.EmailVerified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update user: %w", err)
	}
	return &u, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
