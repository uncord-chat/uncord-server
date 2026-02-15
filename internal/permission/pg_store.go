package permission

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

// PGStore implements Store using PostgreSQL.
type PGStore struct {
	db *pgxpool.Pool
}

// NewPGStore creates a new PostgreSQL-backed permission store.
func NewPGStore(db *pgxpool.Pool) *PGStore {
	return &PGStore{db: db}
}

// IsOwner reports whether the given user is the server owner.
func (s *PGStore) IsOwner(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM server_config WHERE owner_id = $1)",
		userID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check owner: %w", err)
	}
	return exists, nil
}

// RolePermissions returns the server-level permission bitfield for every role the user holds, plus the @everyone role.
func (s *PGStore) RolePermissions(ctx context.Context, userID uuid.UUID) ([]RolePermEntry, error) {
	rows, err := s.db.Query(ctx, `
		SELECT r.id, r.permissions FROM roles r
		JOIN member_roles mr ON mr.role_id = r.id
		WHERE mr.user_id = $1
		UNION
		SELECT r.id, r.permissions FROM roles r
		WHERE r.is_everyone = true
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query role permissions: %w", err)
	}
	defer rows.Close()

	var entries []RolePermEntry
	for rows.Next() {
		var e RolePermEntry
		var perms int64
		if err := rows.Scan(&e.RoleID, &perms); err != nil {
			return nil, fmt.Errorf("scan role permission: %w", err)
		}
		e.Permissions = permissions.Permission(perms)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ChannelInfo returns the channel's ID and optional parent category.
func (s *PGStore) ChannelInfo(ctx context.Context, channelID uuid.UUID) (ChannelInfo, error) {
	var info ChannelInfo
	err := s.db.QueryRow(ctx,
		"SELECT id, category_id FROM channels WHERE id = $1",
		channelID,
	).Scan(&info.ID, &info.CategoryID)
	if err != nil {
		return ChannelInfo{}, fmt.Errorf("query channel info: %w", err)
	}
	return info, nil
}

// Set upserts a permission override. If an override already exists for the given target and principal combination, the
// allow and deny bitfields are updated. The full row is returned after the operation.
func (s *PGStore) Set(ctx context.Context, targetType TargetType, targetID uuid.UUID, principalType PrincipalType, principalID uuid.UUID, allow, deny permissions.Permission) (*OverrideRow, error) {
	var row OverrideRow
	var targetTypeStr, principalTypeStr string
	var allowVal, denyVal int64
	err := s.db.QueryRow(ctx, `
		INSERT INTO permission_overrides (target_type, target_id, principal_type, principal_id, allow, deny)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (target_type, target_id, principal_type, principal_id)
		DO UPDATE SET allow = EXCLUDED.allow, deny = EXCLUDED.deny, updated_at = NOW()
		RETURNING id, target_type, target_id, principal_type, principal_id, allow, deny, created_at, updated_at
	`, string(targetType), targetID, string(principalType), principalID, int64(allow), int64(deny),
	).Scan(&row.ID, &targetTypeStr, &row.TargetID, &principalTypeStr, &row.PrincipalID, &allowVal, &denyVal, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert override: %w", err)
	}
	row.TargetType = TargetType(targetTypeStr)
	row.PrincipalType = PrincipalType(principalTypeStr)
	row.Allow = permissions.Permission(allowVal)
	row.Deny = permissions.Permission(denyVal)
	return &row, nil
}

// Delete removes a permission override. Returns ErrOverrideNotFound if no matching row exists.
func (s *PGStore) Delete(ctx context.Context, targetType TargetType, targetID uuid.UUID, principalType PrincipalType, principalID uuid.UUID) error {
	tag, err := s.db.Exec(ctx,
		"DELETE FROM permission_overrides WHERE target_type = $1 AND target_id = $2 AND principal_type = $3 AND principal_id = $4",
		string(targetType), targetID, string(principalType), principalID,
	)
	if err != nil {
		return fmt.Errorf("delete override: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOverrideNotFound
	}
	return nil
}

// Overrides returns all permission overrides for the given target (channel or category).
func (s *PGStore) Overrides(ctx context.Context, targetType TargetType, targetID uuid.UUID) ([]Override, error) {
	rows, err := s.db.Query(ctx,
		"SELECT principal_type, principal_id, allow, deny FROM permission_overrides WHERE target_type = $1 AND target_id = $2",
		string(targetType), targetID,
	)
	if err != nil {
		return nil, fmt.Errorf("query overrides: %w", err)
	}
	defer rows.Close()

	var overrides []Override
	for rows.Next() {
		var o Override
		var allow, deny int64
		var principalType string
		if err := rows.Scan(&principalType, &o.PrincipalID, &allow, &deny); err != nil {
			return nil, fmt.Errorf("scan override: %w", err)
		}
		o.PrincipalType = PrincipalType(principalType)
		o.Allow = permissions.Permission(allow)
		o.Deny = permissions.Permission(deny)
		overrides = append(overrides, o)
	}
	return overrides, rows.Err()
}
