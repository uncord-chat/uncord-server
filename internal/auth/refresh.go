package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Valkey key patterns:
//
//	refresh:{token}        → user_id (STRING with TTL)
//	user_refresh:{user_id} → SET of token UUIDs

func refreshKey(token string) string {
	return "refresh:" + token
}

func userRefreshKey(userID uuid.UUID) string {
	return "user_refresh:" + userID.String()
}

// rotateScript atomically consumes an old refresh token and issues a new one. Returns the user ID on success, or nil
// if the old token was not found (indicating reuse). Also cleans up expired token UUIDs from the user set so it does
// not grow unboundedly when tokens expire naturally without being rotated or revoked.
//
//	KEYS[1] = refresh:{oldToken}
//	ARGV[1] = oldToken (UUID string, for SREM from user set)
//	ARGV[2] = newToken (UUID string)
//	ARGV[3] = TTL in seconds
var rotateScript = redis.NewScript(`
local userId = redis.call('GET', KEYS[1])
if not userId then
    return false
end

redis.call('DEL', KEYS[1])

local userSetKey = 'user_refresh:' .. userId
redis.call('SREM', userSetKey, ARGV[1])

-- Remove stale entries whose refresh:{token} key has expired.
local members = redis.call('SMEMBERS', userSetKey)
for _, member in ipairs(members) do
    if redis.call('EXISTS', 'refresh:' .. member) == 0 then
        redis.call('SREM', userSetKey, member)
    end
end

local newKey = 'refresh:' .. ARGV[2]
redis.call('SET', newKey, userId, 'EX', tonumber(ARGV[3]))
redis.call('SADD', userSetKey, ARGV[2])
redis.call('EXPIRE', userSetKey, tonumber(ARGV[3]))

return userId
`)

// createScript atomically stores a new refresh token and adds it to the user's token set, ensuring both operations
// succeed or fail together. Expired token UUIDs are cleaned from the set to prevent unbounded growth.
//
//	KEYS[1] = refresh:{token}
//	KEYS[2] = user_refresh:{userID}
//	ARGV[1] = userID string
//	ARGV[2] = token UUID string (for SADD into user set)
//	ARGV[3] = TTL in seconds
var createScript = redis.NewScript(`
-- Remove stale entries whose refresh:{token} key has expired.
local members = redis.call('SMEMBERS', KEYS[2])
for _, member in ipairs(members) do
    if redis.call('EXISTS', 'refresh:' .. member) == 0 then
        redis.call('SREM', KEYS[2], member)
    end
end

redis.call('SET', KEYS[1], ARGV[1], 'EX', tonumber(ARGV[3]))
redis.call('SADD', KEYS[2], ARGV[2])
redis.call('EXPIRE', KEYS[2], tonumber(ARGV[3]))
return 1
`)

// revokeAllScript atomically removes all refresh tokens for a user.
//
//	KEYS[1] = user_refresh:{userID}
var revokeAllScript = redis.NewScript(`
local tokens = redis.call('SMEMBERS', KEYS[1])
for _, token in ipairs(tokens) do
    redis.call('DEL', 'refresh:' .. token)
end
redis.call('DEL', KEYS[1])
return #tokens
`)

// CreateRefreshToken generates a new refresh token for the user and stores it in Valkey with the given TTL. A Lua
// script ensures the token key and user set are updated atomically.
func CreateRefreshToken(ctx context.Context, rdb *redis.Client, userID uuid.UUID, ttl time.Duration) (string, error) {
	token := uuid.New().String()

	_, err := createScript.Run(ctx, rdb,
		[]string{refreshKey(token), userRefreshKey(userID)},
		userID.String(), token, int(ttl.Seconds()),
	).Result()
	if err != nil {
		return "", fmt.Errorf("create refresh token: %w", err)
	}

	return token, nil
}

// ValidateRefreshToken checks whether a refresh token exists in Valkey and returns the associated user ID.
func ValidateRefreshToken(ctx context.Context, rdb *redis.Client, token string) (uuid.UUID, error) {
	val, err := rdb.Get(ctx, refreshKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return uuid.Nil, ErrRefreshTokenNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("get refresh token: %w", err)
	}

	userID, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse user ID from refresh token: %w", err)
	}

	return userID, nil
}

// RotateRefreshToken atomically consumes an old refresh token and issues a new one via a Lua script. If the old token
// was already consumed, returns ErrRefreshTokenReused.
func RotateRefreshToken(ctx context.Context, rdb *redis.Client, oldToken string, ttl time.Duration) (string, uuid.UUID, error) {
	newToken := uuid.New().String()

	result, err := rotateScript.Run(ctx, rdb,
		[]string{refreshKey(oldToken)},
		oldToken, newToken, int(ttl.Seconds()),
	).Text()

	if errors.Is(err, redis.Nil) {
		return "", uuid.Nil, ErrRefreshTokenReused
	}
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("rotate refresh token: %w", err)
	}

	userID, err := uuid.Parse(result)
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("parse user ID from refresh token: %w", err)
	}

	return newToken, userID, nil
}

// RevokeAllRefreshTokens atomically removes all refresh tokens for the given user via a Lua script.
func RevokeAllRefreshTokens(ctx context.Context, rdb *redis.Client, userID uuid.UUID) error {
	_, err := revokeAllScript.Run(ctx, rdb,
		[]string{userRefreshKey(userID)},
	).Result()

	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("revoke refresh tokens: %w", err)
	}

	return nil
}
