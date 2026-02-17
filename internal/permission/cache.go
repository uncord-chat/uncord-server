package permission

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

const (
	// CacheTTL is the default time-to-live for cached permission values.
	CacheTTL = 300 * time.Second

	// CachePrefix is the key prefix for cached permissions in Valkey.
	CachePrefix = "perms"

	// InvalidateChannel is the pub/sub channel for cache invalidation.
	InvalidateChannel = "uncord.cache.invalidate"

	// scanBatchSize is the number of keys to retrieve per SCAN iteration.
	scanBatchSize = 100
)

func cacheKey(userID, channelID uuid.UUID) string {
	return CachePrefix + ":" + userID.String() + ":" + channelID.String()
}

// Cache provides get/set/delete operations for computed permission values.
type Cache interface {
	Get(ctx context.Context, userID, channelID uuid.UUID) (permissions.Permission, bool, error)
	Set(ctx context.Context, userID, channelID uuid.UUID, perm permissions.Permission) error
	GetMany(ctx context.Context, userID uuid.UUID, channelIDs []uuid.UUID) (map[uuid.UUID]permissions.Permission, error)
	SetMany(ctx context.Context, userID uuid.UUID, perms map[uuid.UUID]permissions.Permission) error
	GetManyUsers(ctx context.Context, userIDs []uuid.UUID, channelID uuid.UUID) (map[uuid.UUID]permissions.Permission, error)
	SetManyUsers(ctx context.Context, channelID uuid.UUID, perms map[uuid.UUID]permissions.Permission) error
	DeleteByUser(ctx context.Context, userID uuid.UUID) error
	DeleteByChannel(ctx context.Context, channelID uuid.UUID) error
	DeleteExact(ctx context.Context, userID, channelID uuid.UUID) error
	DeleteAll(ctx context.Context) error
}

// ValkeyCache implements Cache using Valkey/Redis.
type ValkeyCache struct {
	client *redis.Client
}

// NewValkeyCache creates a new Valkey-backed permission cache.
func NewValkeyCache(client *redis.Client) *ValkeyCache {
	return &ValkeyCache{client: client}
}

func (c *ValkeyCache) Get(ctx context.Context, userID, channelID uuid.UUID) (permissions.Permission, bool, error) {
	val, err := c.client.Get(ctx, cacheKey(userID, channelID)).Result()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("cache get: %w", err)
	}

	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse cached permission: %w", err)
	}

	return permissions.Permission(n), true, nil
}

func (c *ValkeyCache) Set(ctx context.Context, userID, channelID uuid.UUID, perm permissions.Permission) error {
	err := c.client.Set(ctx, cacheKey(userID, channelID), strconv.FormatInt(int64(perm), 10), CacheTTL).Err()
	if err != nil {
		return fmt.Errorf("cache set: %w", err)
	}
	return nil
}

// GetMany retrieves cached permissions for multiple channels in a single MGET round trip. The returned map contains
// only the channels that were found in the cache; missing channels are omitted.
func (c *ValkeyCache) GetMany(ctx context.Context, userID uuid.UUID, channelIDs []uuid.UUID) (map[uuid.UUID]permissions.Permission, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}

	keys := make([]string, len(channelIDs))
	for i, chID := range channelIDs {
		keys[i] = cacheKey(userID, chID)
	}

	vals, err := c.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("cache mget: %w", err)
	}

	result := make(map[uuid.UUID]permissions.Permission, len(channelIDs))
	for i, val := range vals {
		if val == nil {
			continue
		}
		s, ok := val.(string)
		if !ok {
			continue
		}
		n, parseErr := strconv.ParseInt(s, 10, 64)
		if parseErr != nil {
			continue
		}
		result[channelIDs[i]] = permissions.Permission(n)
	}

	return result, nil
}

// SetMany writes multiple permission entries in a single pipelined round trip.
func (c *ValkeyCache) SetMany(ctx context.Context, userID uuid.UUID, perms map[uuid.UUID]permissions.Permission) error {
	if len(perms) == 0 {
		return nil
	}

	pipe := c.client.Pipeline()
	for chID, perm := range perms {
		pipe.Set(ctx, cacheKey(userID, chID), strconv.FormatInt(int64(perm), 10), CacheTTL)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("cache pipeline set: %w", err)
	}
	return nil
}

// GetManyUsers retrieves cached permissions for multiple users in a single channel via a batched MGET round trip. The
// returned map contains only the users that were found in the cache; missing users are omitted.
func (c *ValkeyCache) GetManyUsers(ctx context.Context, userIDs []uuid.UUID, channelID uuid.UUID) (map[uuid.UUID]permissions.Permission, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	keys := make([]string, len(userIDs))
	for i, uid := range userIDs {
		keys[i] = cacheKey(uid, channelID)
	}

	vals, err := c.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("cache mget users: %w", err)
	}

	result := make(map[uuid.UUID]permissions.Permission, len(userIDs))
	for i, val := range vals {
		if val == nil {
			continue
		}
		s, ok := val.(string)
		if !ok {
			continue
		}
		n, parseErr := strconv.ParseInt(s, 10, 64)
		if parseErr != nil {
			continue
		}
		result[userIDs[i]] = permissions.Permission(n)
	}

	return result, nil
}

// SetManyUsers writes permission entries for multiple users in a single channel via a pipelined round trip.
func (c *ValkeyCache) SetManyUsers(ctx context.Context, channelID uuid.UUID, perms map[uuid.UUID]permissions.Permission) error {
	if len(perms) == 0 {
		return nil
	}

	pipe := c.client.Pipeline()
	for uid, perm := range perms {
		pipe.Set(ctx, cacheKey(uid, channelID), strconv.FormatInt(int64(perm), 10), CacheTTL)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("cache pipeline set users: %w", err)
	}
	return nil
}

func (c *ValkeyCache) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	return c.scanAndDelete(ctx, CachePrefix+":"+userID.String()+":*")
}

func (c *ValkeyCache) DeleteByChannel(ctx context.Context, channelID uuid.UUID) error {
	return c.scanAndDelete(ctx, CachePrefix+":*:"+channelID.String())
}

func (c *ValkeyCache) DeleteExact(ctx context.Context, userID, channelID uuid.UUID) error {
	return c.client.Del(ctx, cacheKey(userID, channelID)).Err()
}

func (c *ValkeyCache) DeleteAll(ctx context.Context) error {
	return c.scanAndDelete(ctx, CachePrefix+":*")
}

func (c *ValkeyCache) scanAndDelete(ctx context.Context, pattern string) error {
	var cursor uint64
	for {
		keys, next, err := c.client.Scan(ctx, cursor, pattern, scanBatchSize).Result()
		if err != nil {
			return fmt.Errorf("scan keys %q: %w", pattern, err)
		}
		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("delete keys: %w", err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}
