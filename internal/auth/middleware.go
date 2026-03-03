package auth

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// Authentication source values stored in c.Locals("authSource"). Downstream middleware (e.g. CSRF enforcement) reads
// this value to determine whether the request was authenticated via the Authorization header or a cookie.
const (
	AuthSourceHeader = "header"
	AuthSourceCookie = "cookie"
)

// RequireAuth returns Fiber middleware that validates a JWT access token and stores the user ID in c.Locals("userID").
// Credentials are checked in order: first the Authorization header (Bearer token), then the access token cookie. The
// authentication source is stored in c.Locals("authSource") as "header" or "cookie" for downstream middleware (e.g.
// CSRF enforcement) to inspect.
func RequireAuth(secret, issuer string, cfg *config.Config) fiber.Handler {
	return func(c fiber.Ctx) error {
		var tokenStr string
		var authSource string

		if header := c.Get("Authorization"); header != "" {
			const prefix = "Bearer "
			if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
				return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Invalid authorization format")
			}
			tokenStr = header[len(prefix):]
			authSource = AuthSourceHeader
		} else if cookie := c.Cookies(AccessCookieName(cfg)); cookie != "" {
			tokenStr = cookie
			authSource = AuthSourceCookie
		} else {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing authentication credentials")
		}

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
		c.Locals("emailVerified", claims.EmailVerified)
		c.Locals("authSource", authSource)
		return c.Next()
	}
}

// RequireVerifiedEmail returns Fiber middleware that blocks users whose email address has not been verified. Must be
// placed after RequireAuth, which stores the email_verified JWT claim in c.Locals("emailVerified").
func RequireVerifiedEmail() fiber.Handler {
	return func(c fiber.Ctx) error {
		verified, ok := c.Locals("emailVerified").(bool)
		if !ok {
			return httputil.Fail(c, fiber.StatusUnauthorized, apierrors.Unauthorised, "Missing user identity")
		}
		if !verified {
			return httputil.Fail(c, fiber.StatusForbidden, apierrors.EmailNotVerified,
				"Email verification is required")
		}
		return c.Next()
	}
}
