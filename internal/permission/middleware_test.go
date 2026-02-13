package permission

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

func TestMiddlewareAllowed(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()

	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels | permissions.SendMessages},
		},
		chanInfo: ChannelInfo{ID: channelID},
	}
	cache := newFakeCache()
	resolver := NewResolver(store, cache, zerolog.Nop())

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		return c.Next()
	})
	app.Get("/channels/:channelID/test", RequirePermission(resolver, permissions.ViewChannels), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/channels/"+channelID.String()+"/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMiddlewareDenied(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()

	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels},
		},
		chanInfo: ChannelInfo{ID: channelID},
	}
	cache := newFakeCache()
	resolver := NewResolver(store, cache, zerolog.Nop())

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		return c.Next()
	})
	app.Get("/channels/:channelID/test", RequirePermission(resolver, permissions.ManageRoles), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/channels/"+channelID.String()+"/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}

	code := readErrCode(t, resp)
	if code != string(apierrors.MissingPermissions) {
		t.Errorf("error code = %q, want %q", code, apierrors.MissingPermissions)
	}
}

func TestMiddlewareNoAuth(t *testing.T) {
	t.Parallel()
	store := &fakeStore{chanInfo: ChannelInfo{ID: uuid.New()}}
	resolver := NewResolver(store, newFakeCache(), zerolog.Nop())

	app := fiber.New()
	// No auth middleware, so userID is not set
	app.Get("/channels/:channelID/test", RequirePermission(resolver, permissions.ViewChannels), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/channels/"+uuid.New().String()+"/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestMiddlewareInvalidChannelID(t *testing.T) {
	t.Parallel()
	store := &fakeStore{chanInfo: ChannelInfo{ID: uuid.New()}}
	resolver := NewResolver(store, newFakeCache(), zerolog.Nop())

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return c.Next()
	})
	app.Get("/channels/:channelID/test", RequirePermission(resolver, permissions.ViewChannels), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/channels/not-a-uuid/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestMiddlewareMissingChannelID(t *testing.T) {
	t.Parallel()
	store := &fakeStore{chanInfo: ChannelInfo{ID: uuid.New()}}
	resolver := NewResolver(store, newFakeCache(), zerolog.Nop())

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return c.Next()
	})
	// Route without :channelID param
	app.Get("/test", RequirePermission(resolver, permissions.ViewChannels), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestMiddlewareResolverError(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()
	store := &fakeStore{
		isOwnerErr: fmt.Errorf("db down"),
		chanInfo:   ChannelInfo{ID: channelID},
	}
	resolver := NewResolver(store, newFakeCache(), zerolog.Nop())

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return c.Next()
	})
	app.Get("/channels/:channelID/test", RequirePermission(resolver, permissions.ViewChannels), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/channels/"+channelID.String()+"/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusInternalServerError)
	}
}

// --- RequireServerPermission tests ---

func TestServerMiddlewareAllowed(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: uuid.New(), Permissions: permissions.ManageServer},
		},
	}
	resolver := NewResolver(store, newFakeCache(), zerolog.Nop())

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		return c.Next()
	})
	app.Get("/server", RequireServerPermission(resolver, permissions.ManageServer), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/server", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServerMiddlewareDenied(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: uuid.New(), Permissions: permissions.ViewChannels},
		},
	}
	resolver := NewResolver(store, newFakeCache(), zerolog.Nop())

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", userID)
		return c.Next()
	})
	app.Get("/server", RequireServerPermission(resolver, permissions.ManageServer), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/server", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}

	code := readErrCode(t, resp)
	if code != string(apierrors.MissingPermissions) {
		t.Errorf("error code = %q, want %q", code, apierrors.MissingPermissions)
	}
}

func TestServerMiddlewareNoAuth(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	resolver := NewResolver(store, newFakeCache(), zerolog.Nop())

	app := fiber.New()
	app.Get("/server", RequireServerPermission(resolver, permissions.ManageServer), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/server", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestServerMiddlewareResolverError(t *testing.T) {
	t.Parallel()
	store := &fakeStore{isOwnerErr: fmt.Errorf("db down")}
	resolver := NewResolver(store, newFakeCache(), zerolog.Nop())

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return c.Next()
	})
	app.Get("/server", RequireServerPermission(resolver, permissions.ManageServer), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/server", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusInternalServerError)
	}
}

func readErrCode(t *testing.T, resp *http.Response) string {
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
