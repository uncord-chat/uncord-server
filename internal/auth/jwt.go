package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// AccessClaims holds the JWT claims for an access token.
type AccessClaims struct {
	jwt.RegisteredClaims
}

// NewAccessToken creates a signed JWT access token for the given user. The issuer is embedded in the token and must be
// verified during validation.
func NewAccessToken(userID uuid.UUID, secret string, ttl time.Duration, issuer string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("JWT secret must not be empty")
	}
	if issuer == "" {
		return "", fmt.Errorf("JWT issuer must not be empty")
	}

	now := time.Now()
	claims := AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}

	return signed, nil
}

// ValidateAccessToken parses and validates a JWT access token string, enforcing HMAC signing method and issuer claim.
func ValidateAccessToken(tokenStr, secret, issuer string) (*AccessClaims, error) {
	if issuer == "" {
		return nil, fmt.Errorf("JWT issuer must not be empty")
	}

	claims := &AccessClaims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithIssuer(issuer))
	if err != nil {
		return nil, err
	}

	return claims, nil
}
