package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/uncord-chat/uncord-server/internal/config"
)

func productionConfig() *config.Config {
	return &config.Config{
		ServerEnv:     "production",
		JWTAccessTTL:  15 * time.Minute,
		JWTRefreshTTL: 7 * 24 * time.Hour,
	}
}

func developmentConfig() *config.Config {
	return &config.Config{
		ServerEnv:     "development",
		JWTAccessTTL:  15 * time.Minute,
		JWTRefreshTTL: 7 * 24 * time.Hour,
	}
}

func TestCookieNamesProduction(t *testing.T) {
	t.Parallel()
	cfg := productionConfig()

	if got := AccessCookieName(cfg); got != "__Host-access_token" {
		t.Errorf("AccessCookieName() = %q, want %q", got, "__Host-access_token")
	}
	if got := RefreshCookieName(cfg); got != "__Host-refresh_token" {
		t.Errorf("RefreshCookieName() = %q, want %q", got, "__Host-refresh_token")
	}
	if got := CSRFCookieName(cfg); got != "__Host-csrf_token" {
		t.Errorf("CSRFCookieName() = %q, want %q", got, "__Host-csrf_token")
	}
}

func TestCookieNamesDevelopment(t *testing.T) {
	t.Parallel()
	cfg := developmentConfig()

	if got := AccessCookieName(cfg); got != "access_token" {
		t.Errorf("AccessCookieName() = %q, want %q", got, "access_token")
	}
	if got := RefreshCookieName(cfg); got != "refresh_token" {
		t.Errorf("RefreshCookieName() = %q, want %q", got, "refresh_token")
	}
	if got := CSRFCookieName(cfg); got != "csrf_token" {
		t.Errorf("CSRFCookieName() = %q, want %q", got, "csrf_token")
	}
}

func TestSetAuthCookiesProduction(t *testing.T) {
	t.Parallel()
	cfg := productionConfig()

	app := fiber.New()
	app.Get("/test", func(c fiber.Ctx) error {
		SetAuthCookies(c, cfg, "access-tok", "refresh-tok")
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	cookies := resp.Cookies()
	cookieMap := make(map[string]*http.Cookie, len(cookies))
	for _, c := range cookies {
		cookieMap[c.Name] = c
	}

	access := cookieMap["__Host-access_token"]
	if access == nil {
		t.Fatal("missing __Host-access_token cookie")
	}
	if access.Value != "access-tok" {
		t.Errorf("access cookie value = %q, want %q", access.Value, "access-tok")
	}
	if !access.HttpOnly {
		t.Error("access cookie should be HttpOnly")
	}
	if !access.Secure {
		t.Error("access cookie should be Secure in production")
	}
	if access.Path != "/api" {
		t.Errorf("access cookie path = %q, want %q", access.Path, "/api")
	}
	if access.MaxAge != int(cfg.JWTAccessTTL/time.Second) {
		t.Errorf("access cookie MaxAge = %d, want %d", access.MaxAge, int(cfg.JWTAccessTTL/time.Second))
	}

	refresh := cookieMap["__Host-refresh_token"]
	if refresh == nil {
		t.Fatal("missing __Host-refresh_token cookie")
	}
	if refresh.Value != "refresh-tok" {
		t.Errorf("refresh cookie value = %q, want %q", refresh.Value, "refresh-tok")
	}
	if !refresh.HttpOnly {
		t.Error("refresh cookie should be HttpOnly")
	}
	if !refresh.Secure {
		t.Error("refresh cookie should be Secure in production")
	}
	if refresh.Path != "/api/v1/auth/refresh" {
		t.Errorf("refresh cookie path = %q, want %q", refresh.Path, "/api/v1/auth/refresh")
	}
	if refresh.MaxAge != int(cfg.JWTRefreshTTL/time.Second) {
		t.Errorf("refresh cookie MaxAge = %d, want %d", refresh.MaxAge, int(cfg.JWTRefreshTTL/time.Second))
	}

	csrf := cookieMap["__Host-csrf_token"]
	if csrf == nil {
		t.Fatal("missing __Host-csrf_token cookie")
	}
	if csrf.Value == "" {
		t.Error("CSRF cookie value should not be empty")
	}
	if csrf.HttpOnly {
		t.Error("CSRF cookie should NOT be HttpOnly (JavaScript must read it)")
	}
	if !csrf.Secure {
		t.Error("CSRF cookie should be Secure in production")
	}
	if csrf.Path != "/api" {
		t.Errorf("CSRF cookie path = %q, want %q", csrf.Path, "/api")
	}
}

func TestSetAuthCookiesDevelopment(t *testing.T) {
	t.Parallel()
	cfg := developmentConfig()

	app := fiber.New()
	app.Get("/test", func(c fiber.Ctx) error {
		SetAuthCookies(c, cfg, "access-tok", "refresh-tok")
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	cookies := resp.Cookies()
	cookieMap := make(map[string]*http.Cookie, len(cookies))
	for _, c := range cookies {
		cookieMap[c.Name] = c
	}

	access := cookieMap["access_token"]
	if access == nil {
		t.Fatal("missing access_token cookie")
	}
	if access.Secure {
		t.Error("access cookie should NOT be Secure in development")
	}

	refresh := cookieMap["refresh_token"]
	if refresh == nil {
		t.Fatal("missing refresh_token cookie")
	}
	if refresh.Secure {
		t.Error("refresh cookie should NOT be Secure in development")
	}

	csrf := cookieMap["csrf_token"]
	if csrf == nil {
		t.Fatal("missing csrf_token cookie")
	}
	if csrf.Secure {
		t.Error("CSRF cookie should NOT be Secure in development")
	}
}

func TestClearAuthCookies(t *testing.T) {
	t.Parallel()
	cfg := productionConfig()

	app := fiber.New()
	app.Get("/test", func(c fiber.Ctx) error {
		ClearAuthCookies(c, cfg)
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	cookies := resp.Cookies()
	for _, c := range cookies {
		if c.MaxAge != -1 {
			t.Errorf("cookie %q MaxAge = %d, want -1", c.Name, c.MaxAge)
		}
	}

	if len(cookies) != 3 {
		t.Errorf("expected 3 cleared cookies, got %d", len(cookies))
	}
}

func TestRefreshCookiePathScoped(t *testing.T) {
	t.Parallel()
	cfg := productionConfig()

	app := fiber.New()
	app.Get("/test", func(c fiber.Ctx) error {
		SetAuthCookies(c, cfg, "a", "r")
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	for _, c := range resp.Cookies() {
		if c.Name == "__Host-refresh_token" && c.Path != "/api/v1/auth/refresh" {
			t.Errorf("refresh cookie path = %q, want %q", c.Path, "/api/v1/auth/refresh")
		}
	}
}
