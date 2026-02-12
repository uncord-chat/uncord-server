package auth

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// RequireAuth returns Fiber middleware that validates a JWT Bearer token from
// the Authorization header and stores the user ID in c.Locals("userID").
func RequireAuth(secret, issuer string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorized, "Missing authorization header")
		}

		const prefix = "Bearer "
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorized, "Invalid authorization format")
		}
		tokenStr := header[len(prefix):]

		claims, err := ValidateAccessToken(tokenStr, secret, issuer)
		if err != nil {
			code := apierrors.Unauthorized
			message := "Invalid token"

			if errors.Is(err, jwt.ErrTokenExpired) {
				code = apierrors.TokenExpired
				message = "Token has expired"
			}

			return httputil.Fail(c, fiber.StatusUnauthorized, code, message)
		}

		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorized, "Invalid token subject")
		}

		c.Locals("userID", userID)
		return c.Next()
	}
}
