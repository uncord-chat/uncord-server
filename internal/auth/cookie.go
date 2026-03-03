package auth

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/uncord-chat/uncord-server/internal/config"
)

// Cookie name suffixes. In production the __Host- prefix is prepended, which requires Secure, Path=/, and no Domain
// attribute. In development (plain HTTP) browsers reject __Host- cookies, so unprefixed names are used instead.
const (
	accessCookieSuffix  = "access_token"
	refreshCookieSuffix = "refresh_token"
	csrfCookieSuffix    = "csrf_token"
)

// AccessCookieName returns the environment-appropriate cookie name for the access token.
func AccessCookieName(cfg *config.Config) string {
	return cookieName(cfg, accessCookieSuffix)
}

// RefreshCookieName returns the environment-appropriate cookie name for the refresh token.
func RefreshCookieName(cfg *config.Config) string {
	return cookieName(cfg, refreshCookieSuffix)
}

// CSRFCookieName returns the environment-appropriate cookie name for the CSRF token.
func CSRFCookieName(cfg *config.Config) string {
	return cookieName(cfg, csrfCookieSuffix)
}

func cookieName(cfg *config.Config, suffix string) string {
	if cfg.IsDevelopment() {
		return suffix
	}
	return "__Host-" + suffix
}

// csrfTokenBytes is the number of random bytes used to generate CSRF tokens. 32 bytes yields 64 hex characters and
// 256 bits of entropy.
const csrfTokenBytes = 32

// SetAuthCookies sets the access token, refresh token, and CSRF token cookies on the response. The access and CSRF
// cookies are scoped to /api so they are sent with every API request. The refresh cookie is scoped to the refresh
// endpoint to prevent it from being sent unnecessarily.
func SetAuthCookies(c fiber.Ctx, cfg *config.Config, accessToken, refreshToken string) {
	secure := !cfg.IsDevelopment()

	c.Cookie(&fiber.Cookie{
		Name:     AccessCookieName(cfg),
		Value:    accessToken,
		Path:     "/api",
		MaxAge:   int(cfg.JWTAccessTTL / time.Second),
		HTTPOnly: true,
		Secure:   secure,
		SameSite: fiber.CookieSameSiteLaxMode,
	})

	c.Cookie(&fiber.Cookie{
		Name:     RefreshCookieName(cfg),
		Value:    refreshToken,
		Path:     "/api/v1/auth/refresh",
		MaxAge:   int(cfg.JWTRefreshTTL / time.Second),
		HTTPOnly: true,
		Secure:   secure,
		SameSite: fiber.CookieSameSiteStrictMode,
	})

	csrfToken := generateSecureToken(csrfTokenBytes)
	c.Cookie(&fiber.Cookie{
		Name:     CSRFCookieName(cfg),
		Value:    csrfToken,
		Path:     "/api",
		MaxAge:   int(cfg.JWTAccessTTL / time.Second),
		HTTPOnly: false,
		Secure:   secure,
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}

// ClearAuthCookies expires the access token, refresh token, and CSRF token cookies by setting MaxAge to -1.
func ClearAuthCookies(c fiber.Ctx, cfg *config.Config) {
	secure := !cfg.IsDevelopment()

	c.Cookie(&fiber.Cookie{
		Name:     AccessCookieName(cfg),
		Value:    "",
		Path:     "/api",
		MaxAge:   -1,
		HTTPOnly: true,
		Secure:   secure,
		SameSite: fiber.CookieSameSiteLaxMode,
	})

	c.Cookie(&fiber.Cookie{
		Name:     RefreshCookieName(cfg),
		Value:    "",
		Path:     "/api/v1/auth/refresh",
		MaxAge:   -1,
		HTTPOnly: true,
		Secure:   secure,
		SameSite: fiber.CookieSameSiteStrictMode,
	})

	c.Cookie(&fiber.Cookie{
		Name:     CSRFCookieName(cfg),
		Value:    "",
		Path:     "/api",
		MaxAge:   -1,
		HTTPOnly: false,
		Secure:   secure,
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}
