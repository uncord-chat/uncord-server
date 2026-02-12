package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	DB *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed user repository.
func NewPGRepository(db *pgxpool.Pool) *PGRepository {
	return &PGRepository{DB: db}
}

// Create inserts a new user and, when params.VerifyToken is non-empty, an
// email verification row — all inside a single transaction.
func (r *PGRepository) Create(ctx context.Context, params CreateParams) (uuid.UUID, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin create user tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Warn().Err(err).Msg("tx rollback failed")
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

// GetByEmail returns the user matching the given email address.
func (r *PGRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.DB.QueryRow(ctx,
		`SELECT id, password_hash, username, mfa_enabled, email_verified
		 FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.PasswordHash, &u.Username, &u.MFAEnabled, &u.EmailVerified)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query user by email: %w", err)
	}
	u.Email = email
	return &u, nil
}

// VerifyEmail consumes a verification token and marks the user as verified,
// all within a single transaction.
func (r *PGRepository) VerifyEmail(ctx context.Context, token string) (uuid.UUID, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin verify email tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Warn().Err(err).Msg("tx rollback failed")
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
	_, err := r.DB.Exec(ctx,
		`INSERT INTO login_attempts (email, ip_address, success) VALUES ($1, $2::inet, $3)`,
		email, ipAddress, success,
	)
	if err != nil {
		return fmt.Errorf("record login attempt: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
