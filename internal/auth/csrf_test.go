package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/config"
)

func TestRequireCSRF(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{ServerEnv: "development"}
	csrfMW := RequireCSRF(cfg)

	tests := []struct {
		name       string
		method     string
		authSource string
		setCookie  string
		setHeader  string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "GET with cookie auth passes (safe method)",
			method:     http.MethodGet,
			authSource: "cookie",
			wantStatus: http.StatusOK,
		},
		{
			name:       "HEAD with cookie auth passes (safe method)",
			method:     http.MethodHead,
			authSource: "cookie",
			wantStatus: http.StatusOK,
		},
		{
			name:       "OPTIONS with cookie auth passes (safe method)",
			method:     http.MethodOptions,
			authSource: "cookie",
			wantStatus: http.StatusOK,
		},
		{
			name:       "POST with header auth passes (CSRF skip)",
			method:     http.MethodPost,
			authSource: "header",
			wantStatus: http.StatusOK,
		},
		{
			name:       "POST with cookie auth and matching tokens passes",
			method:     http.MethodPost,
			authSource: "cookie",
			setCookie:  "valid-csrf-token",
			setHeader:  "valid-csrf-token",
			wantStatus: http.StatusOK,
		},
		{
			name:       "POST with cookie auth and missing header returns 403",
			method:     http.MethodPost,
			authSource: "cookie",
			setCookie:  "valid-csrf-token",
			setHeader:  "",
			wantStatus: http.StatusForbidden,
			wantCode:   string(apierrors.CSRFTokenMissing),
		},
		{
			name:       "POST with cookie auth and mismatched tokens returns 403",
			method:     http.MethodPost,
			authSource: "cookie",
			setCookie:  "valid-csrf-token",
			setHeader:  "wrong-csrf-token",
			wantStatus: http.StatusForbidden,
			wantCode:   string(apierrors.CSRFTokenInvalid),
		},
		{
			name:       "POST with cookie auth and missing cookie returns 403",
			method:     http.MethodPost,
			authSource: "cookie",
			setCookie:  "",
			setHeader:  "some-token",
			wantStatus: http.StatusForbidden,
			wantCode:   string(apierrors.CSRFTokenMissing),
		},
		{
			name:       "DELETE with cookie auth and matching tokens passes",
			method:     http.MethodDelete,
			authSource: "cookie",
			setCookie:  "csrf-tok",
			setHeader:  "csrf-tok",
			wantStatus: http.StatusOK,
		},
		{
			name:       "PATCH with no authSource passes (not cookie-authenticated)",
			method:     http.MethodPatch,
			authSource: "",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := fiber.New()

			app.Use(func(c fiber.Ctx) error {
				if tt.authSource != "" {
					c.Locals("authSource", tt.authSource)
				}
				return c.Next()
			})

			handler := func(c fiber.Ctx) error {
				return c.SendStatus(http.StatusOK)
			}

			switch tt.method {
			case http.MethodGet:
				app.Get("/test", csrfMW, handler)
			case http.MethodHead:
				app.Head("/test", csrfMW, handler)
			case http.MethodOptions:
				app.Options("/test", csrfMW, handler)
			case http.MethodPost:
				app.Post("/test", csrfMW, handler)
			case http.MethodDelete:
				app.Delete("/test", csrfMW, handler)
			case http.MethodPatch:
				app.Patch("/test", csrfMW, handler)
			}

			req := httptest.NewRequestWithContext(context.Background(),tt.method, "/test", nil)
			if tt.setCookie != "" {
				req.AddCookie(&http.Cookie{Name: CSRFCookieName(cfg), Value: tt.setCookie})
			}
			if tt.setHeader != "" {
				req.Header.Set("X-CSRF-Token", tt.setHeader)
			}

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test() error = %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantCode != "" {
				code := readErrorCode(t, resp)
				if code != tt.wantCode {
					t.Errorf("error code = %q, want %q", code, tt.wantCode)
				}
			}
		})
	}
}
