package auth

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v3"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// RequireCSRF returns Fiber middleware that enforces the double-submit cookie CSRF pattern for cookie-authenticated
// requests. Safe methods (GET, HEAD, OPTIONS) are always allowed through. Requests authenticated via the Authorization
// header (authSource == "header") are skipped because Bearer token authentication is not vulnerable to CSRF.
func RequireCSRF(cfg *config.Config) fiber.Handler {
	return func(c fiber.Ctx) error {
		switch c.Method() {
		case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
			return c.Next()
		}

		source, _ := c.Locals("authSource").(string)
		if source != AuthSourceCookie {
			return c.Next()
		}

		cookieToken := c.Cookies(CSRFCookieName(cfg))
		headerToken := c.Get("X-CSRF-Token")

		if cookieToken == "" || headerToken == "" {
			return httputil.Fail(c, fiber.StatusForbidden, apierrors.CSRFTokenMissing, "CSRF token is required")
		}

		if subtle.ConstantTimeCompare([]byte(cookieToken), []byte(headerToken)) != 1 {
			return httputil.Fail(c, fiber.StatusForbidden, apierrors.CSRFTokenInvalid, "CSRF token mismatch")
		}

		return c.Next()
	}
}
