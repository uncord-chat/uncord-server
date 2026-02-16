package auth

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// RequireAuth returns Fiber middleware that validates a JWT Bearer token from the Authorization header and stores the
// user ID in c.Locals("userID").
func RequireAuth(secret, issuer string) fiber.Handler {
	return func(c fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing authorization header")
		}

		const prefix = "Bearer "
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Invalid authorization format")
		}
		tokenStr := header[len(prefix):]

		claims, err := ValidateAccessToken(tokenStr, secret, issuer)
		if err != nil {
			code := apierrors.Unauthorised
			message := "Invalid token"

			if errors.Is(err, jwt.ErrTokenExpired) {
				code = apierrors.TokenExpired
				message = "Token has expired"
			}

			return httputil.Fail(c, fiber.StatusUnauthorized, code, message)
		}

		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Invalid token subject")
		}

		c.Locals("userID", userID)
		return c.Next()
	}
}

// RequireVerifiedEmail returns Fiber middleware that blocks users whose email address has not been verified. Must be
// placed after RequireAuth so that c.Locals("userID") is populated.
func RequireVerifiedEmail(users user.Repository) fiber.Handler {
	return func(c fiber.Ctx) error {
		userID, ok := c.Locals("userID").(uuid.UUID)
		if !ok {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
		}
		u, err := users.GetByID(c, userID)
		if err != nil {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "User not found")
		}
		if !u.EmailVerified {
			return httputil.Fail(c, fiber.StatusForbidden, apierrors.EmailNotVerified,
				"Email verification is required")
		}
		return c.Next()
	}
}
