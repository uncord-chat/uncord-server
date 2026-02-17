package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// pendingMFATTL is the duration that a pending MFA secret is stored in Valkey before automatic cleanup.
const pendingMFATTL = 10 * time.Minute

// maxMFASetupAttempts limits how many times a user can submit an incorrect TOTP code during MFA setup before the
// pending secret is discarded and the flow must be restarted.
const maxMFASetupAttempts = 5

// Valkey key pattern for pending MFA setup:
//
//	mfa_pending:{user_id} → encrypted_secret (STRING with TTL)

func mfaPendingKey(userID uuid.UUID) string {
	return "mfa_pending:" + userID.String()
}

func mfaPendingAttemptsKey(userID uuid.UUID) string {
	return "mfa_pending_attempts:" + userID.String()
}

// StorePendingMFASecret stores an encrypted TOTP secret in Valkey for the given user, replacing any existing pending
// secret. The secret expires after pendingMFATTL, providing automatic cleanup for abandoned setup flows.
func StorePendingMFASecret(ctx context.Context, rdb *redis.Client, userID uuid.UUID, encryptedSecret string) error {
	err := rdb.Set(ctx, mfaPendingKey(userID), encryptedSecret, pendingMFATTL).Err()
	if err != nil {
		return fmt.Errorf("store pending MFA secret: %w", err)
	}
	return nil
}

// ConsumePendingMFASecret atomically reads and deletes the pending MFA secret for the given user. Returns
// ErrInvalidToken if no pending secret exists.
func ConsumePendingMFASecret(ctx context.Context, rdb *redis.Client, userID uuid.UUID) (string, error) {
	val, err := rdb.GetDel(ctx, mfaPendingKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrInvalidToken
	}
	if err != nil {
		return "", fmt.Errorf("consume pending MFA secret: %w", err)
	}
	return val, nil
}

// IncrementMFASetupAttempts atomically increments the failed attempt counter for MFA setup confirmation. Returns the
// new count. The counter shares the same TTL as the pending secret so it expires together with the setup flow.
func IncrementMFASetupAttempts(ctx context.Context, rdb *redis.Client, userID uuid.UUID) (int64, error) {
	key := mfaPendingAttemptsKey(userID)
	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("increment MFA setup attempts: %w", err)
	}
	if count == 1 {
		if err := rdb.Expire(ctx, key, pendingMFATTL).Err(); err != nil {
			return count, fmt.Errorf("set MFA attempts TTL: %w", err)
		}
	}
	return count, nil
}

// ResetMFASetupAttempts deletes the failed attempt counter, called when a new MFA setup begins.
func ResetMFASetupAttempts(ctx context.Context, rdb *redis.Client, userID uuid.UUID) error {
	if err := rdb.Del(ctx, mfaPendingAttemptsKey(userID)).Err(); err != nil {
		return fmt.Errorf("reset MFA setup attempts: %w", err)
	}
	return nil
}
