package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/httputil"
)

// TestUnknownRouteReturns404 verifies that requests to undefined paths receive a 404 JSON response. Fiber v3 treats
// app.Use() middleware as route matches, so without the catch-all handler at the end of registerRoutes the router would
// return 200 with an empty body for unmatched paths.
func TestUnknownRouteReturns404(t *testing.T) {
	t.Parallel()

	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			status := fiber.StatusInternalServerError
			message := "An internal error occurred"
			apiCode := apierrors.InternalError
			if e, ok := errors.AsType[*fiber.Error](err); ok {
				status = e.Code
				message = e.Message
				apiCode = fiberStatusToAPICode(e.Code)
			}
			return c.Status(status).JSON(httputil.ErrorResponse{
				Error: httputil.ErrorBody{
					Code:    apiCode,
					Message: message,
				},
			})
		},
	})

	// Register middleware so the router has app.Use() handlers that match all paths, reproducing the condition that
	// causes Fiber v3 to treat unmatched requests as handled.
	app.Use(func(c fiber.Ctx) error {
		return c.Next()
	})

	app.Get("/known", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	// Catch-all: mirrors the handler at the end of registerRoutes.
	app.Use(func(_ fiber.Ctx) error {
		return fiber.ErrNotFound
	})

	tests := []struct {
		name string
		path string
		want int
	}{
		{"unknown path", "/no-such-route", fiber.StatusNotFound},
		{"favicon", "/favicon.ico", fiber.StatusNotFound},
		{"known path", "/known", fiber.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, tt.path, nil))
			if err != nil {
				t.Fatalf("app.Test() error = %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.want)
			}

			if tt.want == fiber.StatusNotFound {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				var env struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(body, &env); err != nil {
					t.Fatalf("unmarshal error response: %v", err)
				}
				if env.Error.Code != string(apierrors.NotFound) {
					t.Errorf("error code = %q, want %q", env.Error.Code, apierrors.NotFound)
				}
			}
		})
	}
}

func TestFiberStatusToAPICode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   apierrors.Code
	}{
		{"not found", fiber.StatusNotFound, apierrors.NotFound},
		{"method not allowed", fiber.StatusMethodNotAllowed, apierrors.ValidationError},
		{"too many requests", fiber.StatusTooManyRequests, apierrors.RateLimited},
		{"request entity too large", fiber.StatusRequestEntityTooLarge, apierrors.PayloadTooLarge},
		{"service unavailable", fiber.StatusServiceUnavailable, apierrors.ServiceUnavailable},
		{"generic 4xx falls back to validation error", fiber.StatusConflict, apierrors.ValidationError},
		{"another 4xx", fiber.StatusGone, apierrors.ValidationError},
		{"5xx falls back to internal error", fiber.StatusInternalServerError, apierrors.InternalError},
		{"502 falls back to internal error", fiber.StatusBadGateway, apierrors.InternalError},
		{"unknown status falls back to internal error", 600, apierrors.InternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fiberStatusToAPICode(tt.status)
			if got != tt.want {
				t.Errorf("fiberStatusToAPICode(%d) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}
