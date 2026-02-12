package auth

import (
	"context"
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

// rotateScript atomically consumes an old refresh token and issues a new one.
// Returns the user ID on success, or nil if the old token was not found
// (indicating reuse).
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

local newKey = 'refresh:' .. ARGV[2]
redis.call('SET', newKey, userId, 'EX', tonumber(ARGV[3]))
redis.call('SADD', userSetKey, ARGV[2])
redis.call('EXPIRE', userSetKey, tonumber(ARGV[3]))

return userId
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

// CreateRefreshToken generates a new refresh token for the user and stores it
// in Valkey with the given TTL.
func CreateRefreshToken(ctx context.Context, rdb *redis.Client, userID uuid.UUID, ttl time.Duration) (string, error) {
	token := uuid.New().String()

	pipe := rdb.Pipeline()
	pipe.Set(ctx, refreshKey(token), userID.String(), ttl)
	pipe.SAdd(ctx, userRefreshKey(userID), token)
	pipe.Expire(ctx, userRefreshKey(userID), ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("create refresh token: %w", err)
	}

	return token, nil
}

// ValidateRefreshToken checks whether a refresh token exists in Valkey and
// returns the associated user ID.
func ValidateRefreshToken(ctx context.Context, rdb *redis.Client, token string) (uuid.UUID, error) {
	val, err := rdb.Get(ctx, refreshKey(token)).Result()
	if err == redis.Nil {
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

// RotateRefreshToken atomically consumes an old refresh token and issues a new
// one via a Lua script. If the old token was already consumed, returns
// ErrRefreshTokenReused.
func RotateRefreshToken(ctx context.Context, rdb *redis.Client, oldToken string, ttl time.Duration) (string, uuid.UUID, error) {
	newToken := uuid.New().String()

	result, err := rotateScript.Run(ctx, rdb,
		[]string{refreshKey(oldToken)},
		oldToken, newToken, int(ttl.Seconds()),
	).Text()

	if err == redis.Nil {
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

// RevokeAllRefreshTokens atomically removes all refresh tokens for the given
// user via a Lua script.
func RevokeAllRefreshTokens(ctx context.Context, rdb *redis.Client, userID uuid.UUID) error {
	_, err := revokeAllScript.Run(ctx, rdb,
		[]string{userRefreshKey(userID)},
	).Result()

	if err != nil && err != redis.Nil {
		return fmt.Errorf("revoke refresh tokens: %w", err)
	}

	return nil
}
