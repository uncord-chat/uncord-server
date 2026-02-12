package permission

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

// Override represents a channel or category-level permission override.
type Override struct {
	PrincipalType PrincipalType
	PrincipalID   uuid.UUID
	Allow         permissions.Permission
	Deny          permissions.Permission
}

// ChannelInfo holds a channel's ID and optional category.
type ChannelInfo struct {
	ID         uuid.UUID
	CategoryID *uuid.UUID
}

// RolePermEntry pairs a role ID with its server-level permissions bitfield.
type RolePermEntry struct {
	RoleID      uuid.UUID
	Permissions permissions.Permission
}

// Store provides read access to permission-related data.
type Store interface {
	IsOwner(ctx context.Context, userID uuid.UUID) (bool, error)
	RolePermissions(ctx context.Context, userID uuid.UUID) ([]RolePermEntry, error)
	ChannelInfo(ctx context.Context, channelID uuid.UUID) (ChannelInfo, error)
	Overrides(ctx context.Context, targetType TargetType, targetID uuid.UUID) ([]Override, error)
}

// PGStore implements Store using PostgreSQL.
type PGStore struct {
	DB *pgxpool.Pool
}

// NewPGStore creates a new PostgreSQL-backed permission store.
func NewPGStore(db *pgxpool.Pool) *PGStore {
	return &PGStore{DB: db}
}

// IsOwner reports whether the given user is the server owner.
func (s *PGStore) IsOwner(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	err := s.DB.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM server_config WHERE owner_id = $1)",
		userID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check owner: %w", err)
	}
	return exists, nil
}

// RolePermissions returns the server-level permission bitfield for every role
// the user holds, plus the @everyone role.
func (s *PGStore) RolePermissions(ctx context.Context, userID uuid.UUID) ([]RolePermEntry, error) {
	rows, err := s.DB.Query(ctx, `
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
	err := s.DB.QueryRow(ctx,
		"SELECT id, category_id FROM channels WHERE id = $1",
		channelID,
	).Scan(&info.ID, &info.CategoryID)
	if err != nil {
		return ChannelInfo{}, fmt.Errorf("query channel info: %w", err)
	}
	return info, nil
}

// Overrides returns all permission overrides for the given target (channel or category).
func (s *PGStore) Overrides(ctx context.Context, targetType TargetType, targetID uuid.UUID) ([]Override, error) {
	rows, err := s.DB.Query(ctx,
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
