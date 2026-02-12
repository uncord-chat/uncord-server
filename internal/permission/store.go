package permission

import (
	"context"

	"github.com/google/uuid"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

// TargetType identifies whether a permission override applies to a channel or category.
type TargetType string

const (
	TargetChannel  TargetType = "channel"
	TargetCategory TargetType = "category"
)

// PrincipalType identifies whether a permission override is for a role or user.
type PrincipalType string

const (
	PrincipalRole PrincipalType = "role"
	PrincipalUser PrincipalType = "user"
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
