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

// Valkey key pattern for pending MFA setup:
//
//	mfa_pending:{user_id} → encrypted_secret (STRING with TTL)

func mfaPendingKey(userID uuid.UUID) string {
	return "mfa_pending:" + userID.String()
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
