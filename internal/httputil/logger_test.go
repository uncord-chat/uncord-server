package httputil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/rs/zerolog"
)

func TestRequestLogger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        int
		wantLevel     string
		useRequestID  bool
		wantRequestID bool
	}{
		{name: "200 logs at info", status: 200, wantLevel: "info", useRequestID: true, wantRequestID: true},
		{name: "201 logs at info", status: 201, wantLevel: "info", useRequestID: true, wantRequestID: true},
		{name: "301 logs at info", status: 301, wantLevel: "info", useRequestID: true, wantRequestID: true},
		{name: "400 logs at warn", status: 400, wantLevel: "warn", useRequestID: true, wantRequestID: true},
		{name: "404 logs at warn", status: 404, wantLevel: "warn", useRequestID: true, wantRequestID: true},
		{name: "500 logs at error", status: 500, wantLevel: "error", useRequestID: true, wantRequestID: true},
		{name: "503 logs at error", status: 503, wantLevel: "error", useRequestID: true, wantRequestID: true},
		{name: "no requestid middleware", status: 200, wantLevel: "info", useRequestID: false, wantRequestID: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := zerolog.New(&buf)

			app := fiber.New()
			if tt.useRequestID {
				app.Use(requestid.New())
			}
			app.Use(RequestLogger(logger))
			app.Get("/test", func(c *fiber.Ctx) error {
				return c.SendStatus(tt.status)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test() error: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.ReadAll(resp.Body)

			if resp.StatusCode != tt.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.status)
			}

			var entry map[string]any
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("failed to parse log entry: %v\nraw: %s", err, buf.String())
			}

			if got := entry["level"]; got != tt.wantLevel {
				t.Errorf("level = %q, want %q", got, tt.wantLevel)
			}

			for _, field := range []string{"method", "path", "status", "latency", "ip"} {
				if _, ok := entry[field]; !ok {
					t.Errorf("missing field %q in log entry", field)
				}
			}

			if entry["message"] != "Request" {
				t.Errorf("message = %q, want %q", entry["message"], "Request")
			}

			_, hasRID := entry["request_id"]
			if tt.wantRequestID && !hasRID {
				t.Error("expected request_id field but it was absent")
			}
			if !tt.wantRequestID && hasRID {
				t.Error("unexpected request_id field present")
			}
		})
	}
}
