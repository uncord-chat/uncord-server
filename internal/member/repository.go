package member

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/postgres"
)

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed member repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// memberQuery is the shared SELECT used by List and GetByUserID. It joins members with users and aggregates role IDs
// from member_roles. Pending members are excluded because they have not completed the onboarding flow and should not
// appear in member listings or be targetable by moderation actions.
var memberQuery = `SELECT m.user_id, u.username, u.display_name, u.avatar_key,
       m.nickname, m.status, m.timeout_until, m.joined_at,
       COALESCE(array_agg(mr.role_id) FILTER (WHERE mr.role_id IS NOT NULL), '{}') AS role_ids
FROM members m
JOIN users u ON u.id = m.user_id
LEFT JOIN member_roles mr ON mr.user_id = m.user_id
WHERE m.status != '` + models.MemberStatusPending + `'`

// memberQueryAnyStatus is identical to memberQuery but includes members in any status, including pending. Used by
// CreatePending and Activate which need to return the member profile regardless of onboarding state.
const memberQueryAnyStatus = `SELECT m.user_id, u.username, u.display_name, u.avatar_key,
       m.nickname, m.status, m.timeout_until, m.joined_at,
       COALESCE(array_agg(mr.role_id) FILTER (WHERE mr.role_id IS NOT NULL), '{}') AS role_ids
FROM members m
JOIN users u ON u.id = m.user_id
LEFT JOIN member_roles mr ON mr.user_id = m.user_id
WHERE 1=1`

// List returns members ordered by (joined_at, user_id) using keyset pagination. The cursor is the user_id from the
// last item on the previous page.
func (r *PGRepository) List(ctx context.Context, after *uuid.UUID, limit int) ([]MemberWithProfile, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if after == nil {
		rows, err = r.db.Query(ctx,
			memberQuery+`
GROUP BY m.user_id, u.username, u.display_name, u.avatar_key,
         m.nickname, m.status, m.timeout_until, m.joined_at
ORDER BY m.joined_at, m.user_id
LIMIT $1`, limit)
	} else {
		rows, err = r.db.Query(ctx,
			memberQuery+` AND (m.joined_at, m.user_id) > (
      SELECT m2.joined_at, m2.user_id FROM members m2 WHERE m2.user_id = $1
  )
GROUP BY m.user_id, u.username, u.display_name, u.avatar_key,
         m.nickname, m.status, m.timeout_until, m.joined_at
ORDER BY m.joined_at, m.user_id
LIMIT $2`, *after, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("query members: %w", err)
	}
	defer rows.Close()

	var members []MemberWithProfile
	for rows.Next() {
		m, err := scanMemberWithProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate members: %w", err)
	}
	return members, nil
}

// GetByUserID returns a single member by user ID.
func (r *PGRepository) GetByUserID(ctx context.Context, userID uuid.UUID) (*MemberWithProfile, error) {
	row := r.db.QueryRow(ctx,
		memberQuery+` AND m.user_id = $1
GROUP BY m.user_id, u.username, u.display_name, u.avatar_key,
         m.nickname, m.status, m.timeout_until, m.joined_at`, userID)

	m, err := scanMemberWithProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query member by user id: %w", err)
	}
	return m, nil
}

// UpdateNickname sets or clears a member's nickname and returns the updated profile.
func (r *PGRepository) UpdateNickname(ctx context.Context, userID uuid.UUID, nickname *string) (*MemberWithProfile, error) {
	tag, err := r.db.Exec(ctx, "UPDATE members SET nickname = $1 WHERE user_id = $2", nickname, userID)
	if err != nil {
		return nil, fmt.Errorf("update nickname: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return r.GetByUserID(ctx, userID)
}

// Delete removes a member record. The member_roles rows cascade automatically.
func (r *PGRepository) Delete(ctx context.Context, userID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM members WHERE user_id = $1", userID)
	if err != nil {
		return fmt.Errorf("delete member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTimeout applies a timeout to a member and returns the updated profile.
func (r *PGRepository) SetTimeout(ctx context.Context, userID uuid.UUID, until time.Time) (*MemberWithProfile, error) {
	tag, err := r.db.Exec(ctx,
		"UPDATE members SET status = $1, timeout_until = $2 WHERE user_id = $3",
		models.MemberStatusTimedOut, until, userID)
	if err != nil {
		return nil, fmt.Errorf("set timeout: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return r.GetByUserID(ctx, userID)
}

// ClearTimeout removes a member's timeout and returns the updated profile.
func (r *PGRepository) ClearTimeout(ctx context.Context, userID uuid.UUID) (*MemberWithProfile, error) {
	tag, err := r.db.Exec(ctx,
		"UPDATE members SET status = $1, timeout_until = NULL WHERE user_id = $2",
		models.MemberStatusActive, userID)
	if err != nil {
		return nil, fmt.Errorf("clear timeout: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return r.GetByUserID(ctx, userID)
}

// Ban inserts a ban record and removes the member in a single transaction. Returns ErrAlreadyBanned if a ban already
// exists for the user.
func (r *PGRepository) Ban(ctx context.Context, userID, bannedBy uuid.UUID, reason *string, expiresAt *time.Time) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ban tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			r.log.Warn().Err(err).Msg("ban tx rollback failed")
		}
	}()

	_, err = tx.Exec(ctx,
		"INSERT INTO bans (user_id, reason, banned_by, expires_at) VALUES ($1, $2, $3, $4)",
		userID, reason, bannedBy, expiresAt)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return ErrAlreadyBanned
		}
		return fmt.Errorf("insert ban: %w", err)
	}

	_, err = tx.Exec(ctx, "DELETE FROM members WHERE user_id = $1", userID)
	if err != nil {
		return fmt.Errorf("remove member on ban: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ban tx: %w", err)
	}
	return nil
}

// Unban removes a ban record. Returns ErrBanNotFound if no ban exists.
func (r *PGRepository) Unban(ctx context.Context, userID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM bans WHERE user_id = $1", userID)
	if err != nil {
		return fmt.Errorf("delete ban: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBanNotFound
	}
	return nil
}

// ListBans returns all ban records joined with the banned user's public profile, ordered by creation time descending.
func (r *PGRepository) ListBans(ctx context.Context) ([]BanRecord, error) {
	rows, err := r.db.Query(ctx,
		`SELECT b.user_id, u.username, u.display_name, u.avatar_key,
		        b.reason, b.banned_by, b.expires_at, b.created_at
		 FROM bans b
		 JOIN users u ON u.id = b.user_id
		 ORDER BY b.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query bans: %w", err)
	}
	defer rows.Close()

	var bans []BanRecord
	for rows.Next() {
		var b BanRecord
		if err := rows.Scan(&b.UserID, &b.Username, &b.DisplayName, &b.AvatarKey,
			&b.Reason, &b.BannedBy, &b.ExpiresAt, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ban: %w", err)
		}
		bans = append(bans, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bans: %w", err)
	}
	return bans, nil
}

// IsBanned checks whether a ban record exists for the given user.
func (r *PGRepository) IsBanned(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM bans WHERE user_id = $1)", userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check ban: %w", err)
	}
	return exists, nil
}

// AssignRole inserts a member_roles record. Returns ErrAlreadyMember (as a role assignment conflict) on unique
// violation.
func (r *PGRepository) AssignRole(ctx context.Context, userID, roleID uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		"INSERT INTO member_roles (user_id, role_id) VALUES ($1, $2)", userID, roleID)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return ErrAlreadyMember
		}
		return fmt.Errorf("assign role: %w", err)
	}
	return nil
}

// RemoveRole deletes a member_roles record. Returns ErrNotFound if the user did not hold the role.
func (r *PGRepository) RemoveRole(ctx context.Context, userID, roleID uuid.UUID) error {
	tag, err := r.db.Exec(ctx,
		"DELETE FROM member_roles WHERE user_id = $1 AND role_id = $2", userID, roleID)
	if err != nil {
		return fmt.Errorf("remove role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreatePending inserts a member with pending status, assigns the @everyone role, and returns the full profile. Returns
// ErrAlreadyMember if the user already has a membership record.
func (r *PGRepository) CreatePending(ctx context.Context, userID uuid.UUID) (*MemberWithProfile, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create pending member tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			r.log.Warn().Err(err).Msg("create pending member tx rollback failed")
		}
	}()

	_, err = tx.Exec(ctx, "INSERT INTO members (user_id, status) VALUES ($1, $2)", userID, models.MemberStatusPending)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, ErrAlreadyMember
		}
		return nil, fmt.Errorf("insert pending member: %w", err)
	}

	_, err = tx.Exec(ctx,
		"INSERT INTO member_roles (user_id, role_id) SELECT $1, id FROM roles WHERE is_everyone = true",
		userID)
	if err != nil {
		return nil, fmt.Errorf("assign everyone role: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create pending member tx: %w", err)
	}

	return r.getByUserIDAnyStatus(ctx, userID)
}

// Activate transitions a pending member to active status, assigns auto-roles, and returns the updated profile. Returns
// ErrNotPending if the member is not in pending status.
func (r *PGRepository) Activate(ctx context.Context, userID uuid.UUID, autoRoles []uuid.UUID) (*MemberWithProfile, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin activate member tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			r.log.Warn().Err(err).Msg("activate member tx rollback failed")
		}
	}()

	tag, err := tx.Exec(ctx,
		"UPDATE members SET status = $1, onboarded_at = NOW() WHERE user_id = $2 AND status = $3",
		models.MemberStatusActive, userID, models.MemberStatusPending)
	if err != nil {
		return nil, fmt.Errorf("activate member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotPending
	}

	for _, roleID := range autoRoles {
		_, err := tx.Exec(ctx,
			"INSERT INTO member_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			userID, roleID)
		if err != nil {
			return nil, fmt.Errorf("assign auto-role: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit activate member tx: %w", err)
	}

	return r.getByUserIDAnyStatus(ctx, userID)
}

// getByUserIDAnyStatus returns a member profile regardless of status, including pending members.
func (r *PGRepository) getByUserIDAnyStatus(ctx context.Context, userID uuid.UUID) (*MemberWithProfile, error) {
	row := r.db.QueryRow(ctx,
		memberQueryAnyStatus+` AND m.user_id = $1
GROUP BY m.user_id, u.username, u.display_name, u.avatar_key,
         m.nickname, m.status, m.timeout_until, m.joined_at`, userID)

	m, err := scanMemberWithProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query member by user id (any status): %w", err)
	}
	return m, nil
}

// scanMemberWithProfile scans a row into a MemberWithProfile.
func scanMemberWithProfile(row pgx.Row) (*MemberWithProfile, error) {
	var m MemberWithProfile
	err := row.Scan(
		&m.UserID, &m.Username, &m.DisplayName, &m.AvatarKey,
		&m.Nickname, &m.Status, &m.TimeoutUntil, &m.JoinedAt,
		&m.RoleIDs,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}
