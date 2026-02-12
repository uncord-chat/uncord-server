package permission

import (
	"context"
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
	DeleteByUser(ctx context.Context, userID uuid.UUID) error
	DeleteByChannel(ctx context.Context, channelID uuid.UUID) error
	DeleteExact(ctx context.Context, userID, channelID uuid.UUID) error
}

// ValkeyCache implements Cache using Valkey/Redis.
type ValkeyCache struct {
	Client *redis.Client
}

// NewValkeyCache creates a new Valkey-backed permission cache.
func NewValkeyCache(client *redis.Client) *ValkeyCache {
	return &ValkeyCache{Client: client}
}

func (c *ValkeyCache) Get(ctx context.Context, userID, channelID uuid.UUID) (permissions.Permission, bool, error) {
	val, err := c.Client.Get(ctx, cacheKey(userID, channelID)).Result()
	if err == redis.Nil {
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
	err := c.Client.Set(ctx, cacheKey(userID, channelID), strconv.FormatInt(int64(perm), 10), CacheTTL).Err()
	if err != nil {
		return fmt.Errorf("cache set: %w", err)
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
	return c.Client.Del(ctx, cacheKey(userID, channelID)).Err()
}

func (c *ValkeyCache) scanAndDelete(ctx context.Context, pattern string) error {
	var cursor uint64
	for {
		keys, next, err := c.Client.Scan(ctx, cursor, pattern, scanBatchSize).Result()
		if err != nil {
			return fmt.Errorf("scan keys %q: %w", pattern, err)
		}
		if len(keys) > 0 {
			if err := c.Client.Del(ctx, keys...).Err(); err != nil {
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
