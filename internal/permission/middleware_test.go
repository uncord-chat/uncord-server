package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

// --- Fake resolver for middleware tests ---

type fakeResolver struct {
	allowed bool
	err     error
}

func (f *fakeResolver) fakeStore() *fakeStore {
	return &fakeStore{
		roleEntries: []RolePermEntry{},
		chanInfo:    ChannelInfo{ID: uuid.New()},
	}
}

func newFakeResolver(allowed bool, err error) *Resolver {
	fr := &fakeResolver{allowed: allowed, err: err}
	store := fr.fakeStore()
	cache := newFakeCache()

	if allowed {
		store.roleEntries = []RolePermEntry{
			{RoleID: uuid.New(), Permissions: permissions.AllPermissions},
		}
	}

	r := NewResolver(store, cache)
	return r
}

// overrideResolver creates a resolver with a controllable store/cache for middleware tests
type testResolver struct {
	result permissions.Permission
	err    error
}

func (r *testResolver) HasPermission(_ context.Context, _, _ uuid.UUID, perm permissions.Permission) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	return r.result.Has(perm), nil
}

func setupMiddlewareApp(t *testing.T, resolver *Resolver, perm permissions.Permission) *fiber.App {
	t.Helper()
	app := fiber.New()
	app.Get("/channels/:channelID/test", RequirePermission(resolver, perm), func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})
	return app
}

func TestMiddlewareAllowed(t *testing.T) {
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
	resolver := NewResolver(store, cache)

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("userID", userID)
		return c.Next()
	})
	app.Get("/channels/:channelID/test", RequirePermission(resolver, permissions.ViewChannels), func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/channels/"+channelID.String()+"/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMiddlewareDenied(t *testing.T) {
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
	resolver := NewResolver(store, cache)

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("userID", userID)
		return c.Next()
	})
	app.Get("/channels/:channelID/test", RequirePermission(resolver, permissions.ManageRoles), func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/channels/"+channelID.String()+"/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}

	code := readErrCode(t, resp)
	if code != "MISSING_PERMISSIONS" {
		t.Errorf("error code = %q, want MISSING_PERMISSIONS", code)
	}
}

func TestMiddlewareNoAuth(t *testing.T) {
	store := &fakeStore{chanInfo: ChannelInfo{ID: uuid.New()}}
	resolver := NewResolver(store, newFakeCache())

	app := fiber.New()
	// No auth middleware — userID not set
	app.Get("/channels/:channelID/test", RequirePermission(resolver, permissions.ViewChannels), func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/channels/"+uuid.New().String()+"/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestMiddlewareInvalidChannelID(t *testing.T) {
	store := &fakeStore{chanInfo: ChannelInfo{ID: uuid.New()}}
	resolver := NewResolver(store, newFakeCache())

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return c.Next()
	})
	app.Get("/channels/:channelID/test", RequirePermission(resolver, permissions.ViewChannels), func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/channels/not-a-uuid/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestMiddlewareMissingChannelID(t *testing.T) {
	store := &fakeStore{chanInfo: ChannelInfo{ID: uuid.New()}}
	resolver := NewResolver(store, newFakeCache())

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return c.Next()
	})
	// Route without :channelID param
	app.Get("/test", RequirePermission(resolver, permissions.ViewChannels), func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}
}

func TestMiddlewareResolverError(t *testing.T) {
	channelID := uuid.New()
	store := &fakeStore{
		isOwnerErr: fmt.Errorf("db down"),
		chanInfo:   ChannelInfo{ID: channelID},
	}
	resolver := NewResolver(store, newFakeCache())

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("userID", uuid.New())
		return c.Next()
	})
	app.Get("/channels/:channelID/test", RequirePermission(resolver, permissions.ViewChannels), func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/channels/"+channelID.String()+"/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
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
