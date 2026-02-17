package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStoreAndConsumePendingMFASecret(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()
	secret := "encrypted-secret-data"

	if err := StorePendingMFASecret(ctx, rdb, userID, secret); err != nil {
		t.Fatalf("StorePendingMFASecret() error = %v", err)
	}

	got, err := ConsumePendingMFASecret(ctx, rdb, userID)
	if err != nil {
		t.Fatalf("ConsumePendingMFASecret() error = %v", err)
	}
	if got != secret {
		t.Errorf("ConsumePendingMFASecret() = %q, want %q", got, secret)
	}
}

func TestConsumePendingMFASecretNotFound(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()

	_, err := ConsumePendingMFASecret(ctx, rdb, uuid.New())
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ConsumePendingMFASecret() error = %v, want ErrInvalidToken", err)
	}
}

func TestConsumePendingMFASecretDoubleConsume(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	if err := StorePendingMFASecret(ctx, rdb, userID, "secret"); err != nil {
		t.Fatalf("StorePendingMFASecret() error = %v", err)
	}

	_, err := ConsumePendingMFASecret(ctx, rdb, userID)
	if err != nil {
		t.Fatalf("first ConsumePendingMFASecret() error = %v", err)
	}

	_, err = ConsumePendingMFASecret(ctx, rdb, userID)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("second ConsumePendingMFASecret() error = %v, want ErrInvalidToken", err)
	}
}

func TestStorePendingMFASecretOverwrite(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	if err := StorePendingMFASecret(ctx, rdb, userID, "first"); err != nil {
		t.Fatalf("StorePendingMFASecret() error = %v", err)
	}
	if err := StorePendingMFASecret(ctx, rdb, userID, "second"); err != nil {
		t.Fatalf("StorePendingMFASecret() error = %v", err)
	}

	got, err := ConsumePendingMFASecret(ctx, rdb, userID)
	if err != nil {
		t.Fatalf("ConsumePendingMFASecret() error = %v", err)
	}
	if got != "second" {
		t.Errorf("ConsumePendingMFASecret() = %q, want %q", got, "second")
	}
}

func TestConsumePendingMFASecretExpired(t *testing.T) {
	t.Parallel()
	mr, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	if err := StorePendingMFASecret(ctx, rdb, userID, "secret"); err != nil {
		t.Fatalf("StorePendingMFASecret() error = %v", err)
	}

	mr.FastForward(pendingMFATTL + time.Second)

	_, err := ConsumePendingMFASecret(ctx, rdb, userID)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("ConsumePendingMFASecret() after expiry error = %v, want ErrInvalidToken", err)
	}
}

func TestIncrementMFASetupAttempts(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	for i := int64(1); i <= 3; i++ {
		count, err := IncrementMFASetupAttempts(ctx, rdb, userID)
		if err != nil {
			t.Fatalf("IncrementMFASetupAttempts() call %d error = %v", i, err)
		}
		if count != i {
			t.Errorf("IncrementMFASetupAttempts() call %d = %d, want %d", i, count, i)
		}
	}
}

func TestIncrementMFASetupAttemptsTTL(t *testing.T) {
	t.Parallel()
	mr, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	_, err := IncrementMFASetupAttempts(ctx, rdb, userID)
	if err != nil {
		t.Fatalf("IncrementMFASetupAttempts() error = %v", err)
	}

	key := mfaPendingAttemptsKey(userID)
	ttl := mr.TTL(key)
	if ttl <= 0 {
		t.Errorf("key TTL = %v, want positive", ttl)
	}
	if ttl > pendingMFATTL {
		t.Errorf("key TTL = %v, want <= %v", ttl, pendingMFATTL)
	}
}

func TestResetMFASetupAttempts(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	for i := 0; i < 3; i++ {
		if _, err := IncrementMFASetupAttempts(ctx, rdb, userID); err != nil {
			t.Fatalf("IncrementMFASetupAttempts() error = %v", err)
		}
	}

	if err := ResetMFASetupAttempts(ctx, rdb, userID); err != nil {
		t.Fatalf("ResetMFASetupAttempts() error = %v", err)
	}

	count, err := IncrementMFASetupAttempts(ctx, rdb, userID)
	if err != nil {
		t.Fatalf("IncrementMFASetupAttempts() after reset error = %v", err)
	}
	if count != 1 {
		t.Errorf("IncrementMFASetupAttempts() after reset = %d, want 1", count)
	}
}
