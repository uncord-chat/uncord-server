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

// serverPermKey is a fixed sentinel UUID used as the channel ID component in cache keys for server-level permission
// results. PostgreSQL gen_random_uuid() produces v4 UUIDs, so the nil UUID cannot collide with real channel IDs.
var serverPermKey = uuid.Nil

// ResolveServer returns the effective server-level permissions for a user, using the cache when available. Only steps 1
// (owner bypass) and 2 (role union) apply; channel and category overrides are not relevant at the server level.
func (r *Resolver) ResolveServer(ctx context.Context, userID uuid.UUID) (permissions.Permission, error) {
	perm, ok, err := r.cache.Get(ctx, userID, serverPermKey)
	if err != nil {
		r.log.Warn().Err(err).Msg("Server permission cache get failed, falling through to compute")
	}
	if ok {
		return perm, nil
	}

	perm, _, err = r.basePermissions(ctx, userID)
	if err != nil {
		return 0, err
	}

	if cacheErr := r.cache.Set(ctx, userID, serverPermKey, perm); cacheErr != nil {
		r.log.Warn().Err(cacheErr).Msg("Server permission cache set failed")
	}

	return perm, nil
}

// HasServerPermission checks whether a user has a specific server-level permission.
func (r *Resolver) HasServerPermission(ctx context.Context, userID uuid.UUID, perm permissions.Permission) (bool, error) {
	effective, err := r.ResolveServer(ctx, userID)
	if err != nil {
		return false, err
	}
	return effective.Has(perm), nil
}

// FilterPermitted returns a boolean slice indicating which of the given channels the user has the requested permission
// for. Each element corresponds to the channel at the same index in the input slice. The owner check and role union are
// performed once, then channel/category overrides are applied per channel.
func (r *Resolver) FilterPermitted(ctx context.Context, userID uuid.UUID, channelIDs []uuid.UUID, perm permissions.Permission) ([]bool, error) {
	base, roleIDs, err := r.basePermissions(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := make([]bool, len(channelIDs))

	// Admin (owner or ManageServer) has all permissions in every channel.
	if roleIDs == nil {
		for i := range result {
			result[i] = true
		}
		return result, nil
	}

	// Bulk cache lookup: single MGET round trip instead of N individual GETs.
	cached, cacheErr := r.cache.GetMany(ctx, userID, channelIDs)
	if cacheErr != nil {
		r.log.Warn().Err(cacheErr).Msg("Permission cache batch get failed, falling through to compute")
		cached = nil
	}

	// Compute permissions for cache misses and collect them for a bulk SET.
	toCache := make(map[uuid.UUID]permissions.Permission)
	for i, chID := range channelIDs {
		if effective, ok := cached[chID]; ok {
			result[i] = effective.Has(perm)
			continue
		}

		effective, computeErr := r.channelOverrides(ctx, base, roleIDs, userID, chID)
		if computeErr != nil {
			return nil, computeErr
		}
		result[i] = effective.Has(perm)
		toCache[chID] = effective
	}

	// Bulk cache write: single pipelined round trip for all misses.
	if len(toCache) > 0 {
		if setErr := r.cache.SetMany(ctx, userID, toCache); setErr != nil {
			r.log.Warn().Err(setErr).Msg("Permission cache batch set failed")
		}
	}

	return result, nil
}

// basePermissions performs steps 1 (owner bypass) and 2 (role union) of the permission algorithm. If the user is an
// owner or has ManageServer, it returns AllPermissions with a nil roleIDs map as a signal that no further override
// checks are needed.
func (r *Resolver) basePermissions(ctx context.Context, userID uuid.UUID) (permissions.Permission, map[uuid.UUID]struct{}, error) {
	isOwner, err := r.store.IsOwner(ctx, userID)
	if err != nil {
		return 0, nil, fmt.Errorf("check owner: %w", err)
	}
	if isOwner {
		return permissions.AllPermissions, nil, nil
	}

	roleEntries, err := r.store.RolePermissions(ctx, userID)
	if err != nil {
		return 0, nil, fmt.Errorf("get role permissions: %w", err)
	}

	var base permissions.Permission
	roleIDs := make(map[uuid.UUID]struct{})
	for _, entry := range roleEntries {
		base = base.Add(entry.Permissions)
		roleIDs[entry.RoleID] = struct{}{}
	}

	if base.Has(permissions.ManageServer) {
		return permissions.AllPermissions, nil, nil
	}

	return base, roleIDs, nil
}

// compute runs the 4-step permission algorithm for a single channel.
func (r *Resolver) compute(ctx context.Context, userID, channelID uuid.UUID) (permissions.Permission, error) {
	base, roleIDs, err := r.basePermissions(ctx, userID)
	if err != nil {
		return 0, err
	}

	// Admin (owner or ManageServer) bypasses all overrides.
	if roleIDs == nil {
		return base, nil
	}

	return r.channelOverrides(ctx, base, roleIDs, userID, channelID)
}

// channelOverrides applies steps 3 (category overrides) and 4 (channel overrides) to a base permission set.
func (r *Resolver) channelOverrides(ctx context.Context, base permissions.Permission, roleIDs map[uuid.UUID]struct{}, userID, channelID uuid.UUID) (permissions.Permission, error) {
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
