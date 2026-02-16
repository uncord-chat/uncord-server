package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/user"
)

func TestRequireAuthNoHeader(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	app.Use(RequireAuth("secret", testIssuer))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}

	body := readErrorCode(t, resp)
	if body != string(apierrors.Unauthorised) {
		t.Errorf("error code = %q, want %q", body, apierrors.Unauthorised)
	}
}

func TestRequireAuthBadFormat(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	app.Use(RequireAuth("secret", testIssuer))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
}

func TestRequireAuthExpiredToken(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	secret := "test-secret"
	app.Use(RequireAuth(secret, testIssuer))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	// Create an expired token
	tokenStr, err := NewAccessToken(uuid.New(), secret, -1*time.Second, testIssuer)
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}

	body := readErrorCode(t, resp)
	if body != string(apierrors.TokenExpired) {
		t.Errorf("error code = %q, want %q", body, apierrors.TokenExpired)
	}
}

func TestRequireAuthValid(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	secret := "test-secret"
	userID := uuid.New()

	app.Use(RequireAuth(secret, testIssuer))
	app.Get("/test", func(c fiber.Ctx) error {
		id, ok := c.Locals("userID").(uuid.UUID)
		if !ok {
			return c.Status(500).SendString("userID not found in locals")
		}
		return c.SendString(id.String())
	})

	tokenStr, err := NewAccessToken(userID, secret, 15*time.Minute, testIssuer)
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	if string(bodyBytes) != userID.String() {
		t.Errorf("body = %q, want %q", string(bodyBytes), userID.String())
	}
}

func TestRequireAuthWrongSignature(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	app.Use(RequireAuth("correct-secret", testIssuer))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	tokenStr, _ := NewAccessToken(uuid.New(), "wrong-secret", 15*time.Minute, testIssuer)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

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

// fakeUserLookup implements user.Repository for RequireVerifiedEmail tests. Only GetByID is exercised; all other
// methods panic to surface unintended calls.
type fakeUserLookup struct {
	users map[uuid.UUID]*user.User
}

func (f *fakeUserLookup) GetByID(_ context.Context, id uuid.UUID) (*user.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, user.ErrNotFound
	}
	return u, nil
}

func (f *fakeUserLookup) Create(context.Context, user.CreateParams) (uuid.UUID, error) {
	panic("not implemented")
}
func (f *fakeUserLookup) GetByEmail(context.Context, string) (*user.Credentials, error) {
	panic("not implemented")
}
func (f *fakeUserLookup) GetCredentialsByID(context.Context, uuid.UUID) (*user.Credentials, error) {
	panic("not implemented")
}
func (f *fakeUserLookup) VerifyEmail(context.Context, string) (uuid.UUID, error) {
	panic("not implemented")
}
func (f *fakeUserLookup) ReplaceVerificationToken(context.Context, uuid.UUID, string, time.Time, time.Duration) error {
	panic("not implemented")
}
func (f *fakeUserLookup) RecordLoginAttempt(context.Context, string, string, bool) error {
	panic("not implemented")
}
func (f *fakeUserLookup) UpdatePasswordHash(context.Context, uuid.UUID, string) error {
	panic("not implemented")
}
func (f *fakeUserLookup) Update(context.Context, uuid.UUID, user.UpdateParams) (*user.User, error) {
	panic("not implemented")
}
func (f *fakeUserLookup) EnableMFA(context.Context, uuid.UUID, string, []string) error {
	panic("not implemented")
}
func (f *fakeUserLookup) DisableMFA(context.Context, uuid.UUID) error {
	panic("not implemented")
}
func (f *fakeUserLookup) GetUnusedRecoveryCodes(context.Context, uuid.UUID) ([]user.MFARecoveryCode, error) {
	panic("not implemented")
}
func (f *fakeUserLookup) UseRecoveryCode(context.Context, uuid.UUID) error {
	panic("not implemented")
}
func (f *fakeUserLookup) ReplaceRecoveryCodes(context.Context, uuid.UUID, []string) error {
	panic("not implemented")
}
func (f *fakeUserLookup) DeleteWithTombstones(context.Context, uuid.UUID, []user.Tombstone) error {
	panic("not implemented")
}
func (f *fakeUserLookup) CheckTombstone(context.Context, user.TombstoneType, string) (bool, error) {
	panic("not implemented")
}

func TestRequireVerifiedEmail(t *testing.T) {
	t.Parallel()

	verifiedID := uuid.New()
	unverifiedID := uuid.New()
	unknownID := uuid.New()

	lookup := &fakeUserLookup{
		users: map[uuid.UUID]*user.User{
			verifiedID:   {EmailVerified: true},
			unverifiedID: {EmailVerified: false},
		},
	}
	mw := RequireVerifiedEmail(lookup)

	tests := []struct {
		name       string
		userID     uuid.UUID
		setLocals  bool
		wantStatus int
		wantCode   string
	}{
		{
			name:       "verified user passes through",
			userID:     verifiedID,
			setLocals:  true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "unverified user is blocked",
			userID:     unverifiedID,
			setLocals:  true,
			wantStatus: http.StatusForbidden,
			wantCode:   string(apierrors.EmailNotVerified),
		},
		{
			name:       "unknown user is blocked",
			userID:     unknownID,
			setLocals:  true,
			wantStatus: http.StatusUnauthorized,
			wantCode:   string(apierrors.Unauthorised),
		},
		{
			name:       "missing locals is blocked",
			setLocals:  false,
			wantStatus: http.StatusUnauthorized,
			wantCode:   string(apierrors.Unauthorised),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := fiber.New()

			// Inject the userID local before the middleware under test.
			app.Use(func(c fiber.Ctx) error {
				if tt.setLocals {
					c.Locals("userID", tt.userID)
				}
				return c.Next()
			})
			app.Get("/test", mw, func(c fiber.Ctx) error {
				return c.SendStatus(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
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
