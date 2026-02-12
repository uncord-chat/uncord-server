package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, rdb
}

func TestCreateAndValidateRefreshToken(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()

	token, err := CreateRefreshToken(ctx, rdb, userID, 5*time.Minute)
	if err != nil {
		t.Fatalf("CreateRefreshToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("CreateRefreshToken() returned empty token")
	}

	gotID, err := ValidateRefreshToken(ctx, rdb, token)
	if err != nil {
		t.Fatalf("ValidateRefreshToken() error = %v", err)
	}
	if gotID != userID {
		t.Errorf("ValidateRefreshToken() userID = %v, want %v", gotID, userID)
	}
}

func TestValidateRefreshTokenNotFound(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()

	_, err := ValidateRefreshToken(ctx, rdb, "nonexistent-token")
	if err == nil {
		t.Fatal("ValidateRefreshToken() with nonexistent token should return error")
	}
}

func TestRotateRefreshToken(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()
	ttl := 5 * time.Minute

	oldToken, err := CreateRefreshToken(ctx, rdb, userID, ttl)
	if err != nil {
		t.Fatalf("CreateRefreshToken() error = %v", err)
	}

	newToken, gotID, err := RotateRefreshToken(ctx, rdb, oldToken, ttl)
	if err != nil {
		t.Fatalf("RotateRefreshToken() error = %v", err)
	}
	if gotID != userID {
		t.Errorf("RotateRefreshToken() userID = %v, want %v", gotID, userID)
	}
	if newToken == "" {
		t.Fatal("RotateRefreshToken() returned empty new token")
	}
	if newToken == oldToken {
		t.Error("RotateRefreshToken() returned same token")
	}

	// Old token should be gone
	_, err = ValidateRefreshToken(ctx, rdb, oldToken)
	if err == nil {
		t.Error("old token should be invalid after rotation")
	}

	// New token should be valid
	gotID, err = ValidateRefreshToken(ctx, rdb, newToken)
	if err != nil {
		t.Fatalf("ValidateRefreshToken(newToken) error = %v", err)
	}
	if gotID != userID {
		t.Errorf("ValidateRefreshToken(newToken) userID = %v, want %v", gotID, userID)
	}
}

func TestRotateRefreshTokenReused(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()
	ttl := 5 * time.Minute

	token, err := CreateRefreshToken(ctx, rdb, userID, ttl)
	if err != nil {
		t.Fatalf("CreateRefreshToken() error = %v", err)
	}

	// First rotation succeeds
	_, _, err = RotateRefreshToken(ctx, rdb, token, ttl)
	if err != nil {
		t.Fatalf("first RotateRefreshToken() error = %v", err)
	}

	// Second rotation with same token should fail
	_, _, err = RotateRefreshToken(ctx, rdb, token, ttl)
	if !errors.Is(err, ErrRefreshTokenReused) {
		t.Errorf("second RotateRefreshToken() error = %v, want ErrRefreshTokenReused", err)
	}
}

func TestRevokeAllRefreshTokens(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()
	userID := uuid.New()
	ttl := 5 * time.Minute

	// Create multiple tokens
	token1, _ := CreateRefreshToken(ctx, rdb, userID, ttl)
	token2, _ := CreateRefreshToken(ctx, rdb, userID, ttl)

	err := RevokeAllRefreshTokens(ctx, rdb, userID)
	if err != nil {
		t.Fatalf("RevokeAllRefreshTokens() error = %v", err)
	}

	// Both tokens should be gone
	_, err = ValidateRefreshToken(ctx, rdb, token1)
	if err == nil {
		t.Error("token1 should be invalid after revocation")
	}
	_, err = ValidateRefreshToken(ctx, rdb, token2)
	if err == nil {
		t.Error("token2 should be invalid after revocation")
	}
}

func TestRevokeAllRefreshTokensEmpty(t *testing.T) {
	t.Parallel()
	_, rdb := setupMiniredis(t)
	ctx := context.Background()

	// Revoking tokens for a user with none should not error
	err := RevokeAllRefreshTokens(ctx, rdb, uuid.New())
	if err != nil {
		t.Fatalf("RevokeAllRefreshTokens() with no tokens error = %v", err)
	}
}
