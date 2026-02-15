package permission

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

// ErrOverrideNotFound is returned when a permission override does not exist.
var ErrOverrideNotFound = errors.New("permission override not found")

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

// OverrideRow represents a full permission override row from the database.
type OverrideRow struct {
	ID            uuid.UUID
	TargetType    TargetType
	TargetID      uuid.UUID
	PrincipalType PrincipalType
	PrincipalID   uuid.UUID
	Allow         permissions.Permission
	Deny          permissions.Permission
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// OverrideStore provides write access to permission overrides.
type OverrideStore interface {
	Set(ctx context.Context, targetType TargetType, targetID uuid.UUID, principalType PrincipalType, principalID uuid.UUID, allow, deny permissions.Permission) (*OverrideRow, error)
	Delete(ctx context.Context, targetType TargetType, targetID uuid.UUID, principalType PrincipalType, principalID uuid.UUID) error
}

// Store provides read access to permission-related data.
type Store interface {
	IsOwner(ctx context.Context, userID uuid.UUID) (bool, error)
	RolePermissions(ctx context.Context, userID uuid.UUID) ([]RolePermEntry, error)
	ChannelInfo(ctx context.Context, channelID uuid.UUID) (ChannelInfo, error)
	Overrides(ctx context.Context, targetType TargetType, targetID uuid.UUID) ([]Override, error)
}
