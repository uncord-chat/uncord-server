package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestNewAccessTokenAndValidate(t *testing.T) {
	userID := uuid.New()
	secret := "test-secret-key-for-jwt"

	tokenStr, err := NewAccessToken(userID, secret, 15*time.Minute, "")
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}

	claims, err := ValidateAccessToken(tokenStr, secret, "")
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	if claims.Subject != userID.String() {
		t.Errorf("Subject = %q, want %q", claims.Subject, userID.String())
	}
}

func TestNewAccessTokenEmptySecret(t *testing.T) {
	_, err := NewAccessToken(uuid.New(), "", 15*time.Minute, "")
	if err == nil {
		t.Fatal("NewAccessToken() with empty secret should return error")
	}
}

func TestValidateAccessTokenExpired(t *testing.T) {
	userID := uuid.New()
	secret := "test-secret"

	// Create a token that expired 1 second ago
	now := time.Now()
	claims := AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Minute)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Second)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	_, err = ValidateAccessToken(tokenStr, secret, "")
	if err == nil {
		t.Fatal("ValidateAccessToken() with expired token should return error")
	}
}

func TestValidateAccessTokenWrongSecret(t *testing.T) {
	userID := uuid.New()

	tokenStr, err := NewAccessToken(userID, "correct-secret", 15*time.Minute, "")
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}

	_, err = ValidateAccessToken(tokenStr, "wrong-secret", "")
	if err == nil {
		t.Fatal("ValidateAccessToken() with wrong secret should return error")
	}
}

func TestValidateAccessTokenMalformed(t *testing.T) {
	_, err := ValidateAccessToken("not.a.valid.jwt", "secret", "")
	if err == nil {
		t.Fatal("ValidateAccessToken() with malformed token should return error")
	}
}
