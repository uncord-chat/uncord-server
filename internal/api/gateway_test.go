package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestUpgradeRejectsNonWebSocket(t *testing.T) {
	t.Parallel()

	handler := NewGatewayHandler(nil)

	app := fiber.New()
	app.Get("/api/v1/gateway", handler.Upgrade)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gateway", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUpgradeRequired)
	}
}
