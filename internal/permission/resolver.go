package permission

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

// Resolver computes effective permissions for a user in a channel.
type Resolver struct {
	store Store
	cache Cache
	log   zerolog.Logger
}

// NewResolver creates a new permission resolver.
func NewResolver(store Store, cache Cache, logger zerolog.Logger) *Resolver {
	return &Resolver{store: store, cache: cache, log: logger}
}

// Resolve returns the effective permissions for a user in a channel, using the cache when available.
func (r *Resolver) Resolve(ctx context.Context, userID, channelID uuid.UUID) (permissions.Permission, error) {
	// Check cache first
	perm, ok, err := r.cache.Get(ctx, userID, channelID)
	if err != nil {
		// Cache error is non-fatal; fall through to compute
		r.log.Warn().Err(err).Msg("Permission cache get failed, falling through to compute")
	}
	if ok {
		return perm, nil
	}

	perm, err = r.compute(ctx, userID, channelID)
	if err != nil {
		return 0, err
	}

	// Cache the result (best-effort)
	if cacheErr := r.cache.Set(ctx, userID, channelID, perm); cacheErr != nil {
		r.log.Warn().Err(cacheErr).Msg("Permission cache set failed")
	}

	return perm, nil
}

// HasPermission checks whether a user has a specific permission in a channel.
func (r *Resolver) HasPermission(ctx context.Context, userID, channelID uuid.UUID, perm permissions.Permission) (bool, error) {
	effective, err := r.Resolve(ctx, userID, channelID)
	if err != nil {
		return false, err
	}
	return effective.Has(perm), nil
}

// ResolveServer returns the effective server-level permissions for a user. Only steps 1 (owner bypass) and 2 (role
// union) apply; channel and category overrides are not relevant at the server level.
func (r *Resolver) ResolveServer(ctx context.Context, userID uuid.UUID) (permissions.Permission, error) {
	isOwner, err := r.store.IsOwner(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("check owner: %w", err)
	}
	if isOwner {
		return permissions.AllPermissions, nil
	}

	roleEntries, err := r.store.RolePermissions(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("get role permissions: %w", err)
	}

	var base permissions.Permission
	for _, entry := range roleEntries {
		base = base.Add(entry.Permissions)
	}

	if base.Has(permissions.ManageServer) {
		return permissions.AllPermissions, nil
	}

	return base, nil
}

// HasServerPermission checks whether a user has a specific server-level permission.
func (r *Resolver) HasServerPermission(ctx context.Context, userID uuid.UUID, perm permissions.Permission) (bool, error) {
	effective, err := r.ResolveServer(ctx, userID)
	if err != nil {
		return false, err
	}
	return effective.Has(perm), nil
}

// compute runs the 4-step permission algorithm.
func (r *Resolver) compute(ctx context.Context, userID, channelID uuid.UUID) (permissions.Permission, error) {
	// Step 1: Owner bypass
	isOwner, err := r.store.IsOwner(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("check owner: %w", err)
	}
	if isOwner {
		return permissions.AllPermissions, nil
	}

	// Step 2: Role union
	roleEntries, err := r.store.RolePermissions(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("get role permissions: %w", err)
	}

	var base permissions.Permission
	roleIDs := make(map[uuid.UUID]struct{})
	for _, entry := range roleEntries {
		base = base.Add(entry.Permissions)
		roleIDs[entry.RoleID] = struct{}{}
	}

	// ManageServer = administrator, full permissions
	if base.Has(permissions.ManageServer) {
		return permissions.AllPermissions, nil
	}

	// Step 3: Category overrides
	chanInfo, err := r.store.ChannelInfo(ctx, channelID)
	if err != nil {
		return 0, fmt.Errorf("get channel info: %w", err)
	}

	if chanInfo.CategoryID != nil {
		catOverrides, err := r.store.Overrides(ctx, TargetCategory, *chanInfo.CategoryID)
		if err != nil {
			return 0, fmt.Errorf("get category overrides: %w", err)
		}
		base = applyOverrides(base, catOverrides, roleIDs, userID)
	}

	// Step 4: Channel overrides
	chanOverrides, err := r.store.Overrides(ctx, TargetChannel, channelID)
	if err != nil {
		return 0, fmt.Errorf("get channel overrides: %w", err)
	}
	base = applyOverrides(base, chanOverrides, roleIDs, userID)

	return base, nil
}

// applyOverrides applies permission overrides to a base bitfield. Role overrides for roles the user holds are merged
// first, then the user-specific override is applied on top.
func applyOverrides(base permissions.Permission, overrides []Override, userRoles map[uuid.UUID]struct{}, userID uuid.UUID) permissions.Permission {
	var roleAllow, roleDeny permissions.Permission
	var userOverride *Override

	for i := range overrides {
		o := &overrides[i]
		if o.PrincipalType == PrincipalUser && o.PrincipalID == userID {
			userOverride = o
			continue
		}
		if o.PrincipalType == PrincipalRole {
			if _, held := userRoles[o.PrincipalID]; held {
				roleAllow = roleAllow.Add(o.Allow)
				roleDeny = roleDeny.Add(o.Deny)
			}
		}
	}

	// Apply role overrides: add allow, then remove deny (deny wins at same level)
	base = base.Add(roleAllow)
	base = base.Remove(roleDeny)

	// Apply user-specific override on top (highest precedence)
	if userOverride != nil {
		base = base.Add(userOverride.Allow)
		base = base.Remove(userOverride.Deny)
	}

	return base
}
