package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
)

func TestRequireAuthNoHeader(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuth("secret", ""))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}

	body := readErrorCode(t, resp)
	if body != string(apierrors.Unauthorized) {
		t.Errorf("error code = %q, want %q", body, apierrors.Unauthorized)
	}
}

func TestRequireAuthBadFormat(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuth("secret", ""))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestRequireAuthExpiredToken(t *testing.T) {
	app := fiber.New()
	secret := "test-secret"
	app.Use(RequireAuth(secret, ""))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	// Create an expired token
	tokenStr, err := NewAccessToken(uuid.New(), secret, -1*time.Second, "")
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}

	body := readErrorCode(t, resp)
	if body != string(apierrors.TokenExpired) {
		t.Errorf("error code = %q, want %q", body, apierrors.TokenExpired)
	}
}

func TestRequireAuthValid(t *testing.T) {
	app := fiber.New()
	secret := "test-secret"
	userID := uuid.New()

	app.Use(RequireAuth(secret, ""))
	app.Get("/test", func(c *fiber.Ctx) error {
		id, ok := c.Locals("userID").(uuid.UUID)
		if !ok {
			return c.Status(500).SendString("userID not found in locals")
		}
		return c.SendString(id.String())
	})

	tokenStr, err := NewAccessToken(userID, secret, 15*time.Minute, "")
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	if string(bodyBytes) != userID.String() {
		t.Errorf("body = %q, want %q", string(bodyBytes), userID.String())
	}
}

func TestRequireAuthWrongSignature(t *testing.T) {
	app := fiber.New()
	app.Use(RequireAuth("correct-secret", ""))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	tokenStr, _ := NewAccessToken(uuid.New(), "wrong-secret", 15*time.Minute, "")

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func readErrorCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("unmarshal body %q: %v", string(bodyBytes), err)
	}
	return body.Error.Code
}
