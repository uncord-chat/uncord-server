package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const testIssuer = "https://test.example.com"

func TestNewAccessTokenAndValidate(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	secret := "test-secret-key-for-jwt"

	tokenStr, err := NewAccessToken(userID, secret, 15*time.Minute, testIssuer)
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}

	claims, err := ValidateAccessToken(tokenStr, secret, testIssuer)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	if claims.Subject != userID.String() {
		t.Errorf("Subject = %q, want %q", claims.Subject, userID.String())
	}
}

func TestNewAccessTokenEmptySecret(t *testing.T) {
	t.Parallel()
	_, err := NewAccessToken(uuid.New(), "", 15*time.Minute, testIssuer)
	if err == nil {
		t.Fatal("NewAccessToken() with empty secret should return error")
	}
}

func TestNewAccessTokenEmptyIssuer(t *testing.T) {
	t.Parallel()
	_, err := NewAccessToken(uuid.New(), "secret", 15*time.Minute, "")
	if err == nil {
		t.Fatal("NewAccessToken() with empty issuer should return error")
	}
}

func TestValidateAccessTokenExpired(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	secret := "test-secret"

	// Create a token that expired 1 second ago.
	now := time.Now()
	claims := AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    testIssuer,
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Minute)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Second)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	_, err = ValidateAccessToken(tokenStr, secret, testIssuer)
	if err == nil {
		t.Fatal("ValidateAccessToken() with expired token should return error")
	}
}

func TestValidateAccessTokenWrongSecret(t *testing.T) {
	t.Parallel()
	userID := uuid.New()

	tokenStr, err := NewAccessToken(userID, "correct-secret", 15*time.Minute, testIssuer)
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}

	_, err = ValidateAccessToken(tokenStr, "wrong-secret", testIssuer)
	if err == nil {
		t.Fatal("ValidateAccessToken() with wrong secret should return error")
	}
}

func TestValidateAccessTokenWrongIssuer(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	secret := "test-secret"

	tokenStr, err := NewAccessToken(userID, secret, 15*time.Minute, testIssuer)
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}

	_, err = ValidateAccessToken(tokenStr, secret, "https://wrong.example.com")
	if err == nil {
		t.Fatal("ValidateAccessToken() with wrong issuer should return error")
	}
}

func TestValidateAccessTokenEmptyIssuer(t *testing.T) {
	t.Parallel()
	_, err := ValidateAccessToken("some.token.here", "secret", "")
	if err == nil {
		t.Fatal("ValidateAccessToken() with empty issuer should return error")
	}
}

func TestValidateAccessTokenMalformed(t *testing.T) {
	t.Parallel()
	_, err := ValidateAccessToken("not.a.valid.jwt", "secret", testIssuer)
	if err == nil {
		t.Fatal("ValidateAccessToken() with malformed token should return error")
	}
}
